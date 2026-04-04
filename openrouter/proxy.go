package openrouter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultPort is the port the OpenRouter proxy listens on.
	// CCR uses 3456; proxy uses 3457 to avoid conflicts.
	DefaultPort = 3457

	// maxUsageLogEntries caps the in-memory usage log entries per session.
	maxUsageLogEntries = 1000
	// maxRequestBytes limits request body size to 10 MB.
	maxRequestBytes = 10 * 1024 * 1024
	// safeSessionPattern allows only alphanumeric, hyphens, and underscores.
	safeSessionPattern = `^[a-zA-Z0-9_-]+$`
)

var safeSessionRe = regexp.MustCompile(safeSessionPattern)


// Proxy handles OpenRouter proxying with usage tracking.
// A single Proxy instance serves all named presets (router configs).
type Proxy struct {
	presets   map[string]ProxyConfig // router config name -> config
	presetsMu sync.RWMutex

	usageLogs   map[string][]UsageLogEntry // session -> logs
	usageMutex  sync.RWMutex

	logDir string
	server *http.Server
}

// NewProxy creates a new OpenRouter proxy with no presets yet.
func NewProxy() (*Proxy, error) {
	logDir := filepath.Join(os.Getenv("HOME"), ".claudebar", "openrouter-usage")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("creating usage log directory: %w", err)
	}

	return &Proxy{
		presets:   make(map[string]ProxyConfig),
		usageLogs: make(map[string][]UsageLogEntry),
		logDir:    logDir,
	}, nil
}

// RegisterPreset adds (or replaces) a named router config.
func (p *Proxy) RegisterPreset(name string, cfg ProxyConfig) {
	p.presetsMu.Lock()
	defer p.presetsMu.Unlock()
	p.presets[name] = cfg
}

// UnregisterPreset removes a named router config.
func (p *Proxy) UnregisterPreset(name string) {
	p.presetsMu.Lock()
	defer p.presetsMu.Unlock()
	delete(p.presets, name)
}

// PresetNames returns sorted preset names.
func (p *Proxy) PresetNames() []string {
	p.presetsMu.RLock()
	defer p.presetsMu.RUnlock()
	names := make([]string, 0, len(p.presets))
	for name := range p.presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasPreset returns true if the named preset exists.
func (p *Proxy) HasPreset(name string) bool {
	p.presetsMu.RLock()
	defer p.presetsMu.RUnlock()
	_, ok := p.presets[name]
	return ok
}

// Start starts the HTTP proxy server on the given port.
func (p *Proxy) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", p.handleMessages)
	mux.HandleFunc("/preset/", p.handlePreset)
	mux.HandleFunc("/usage/", p.getUsage)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	})

	p.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	// Don't print to stdout ��� this runs as a goroutine inside tmux run-shell
	// and stdout output shows as tmux messages
	return p.server.ListenAndServe()
}

// Stop gracefully shuts down the proxy server
func (p *Proxy) Stop() {
	if p.server != nil {
		p.server.Close()
	}
}

// handlePreset routes /preset/<name>/v1/messages to the named config.
// This matches CCR's URL format so sessions can point to either seamlessly.
func (p *Proxy) handlePreset(w http.ResponseWriter, r *http.Request) {
	prefix := "/preset/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.SplitN(remainder, "/", 3)
	if len(parts) < 3 || parts[1] != "v1" || parts[2] != "messages" {
		http.Error(w, "invalid preset endpoint", http.StatusNotFound)
		return
	}

	presetName := parts[0]
	p.forwardRequest(w, r, presetName)
}

// handleMessages handles /v1/messages with optional session/preset query params
// or X-Claudebar-Session / X-OpenRouter-Preset headers.
func (p *Proxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	presetName := r.Header.Get("X-OpenRouter-Preset")
	if presetName == "" {
		presetName = r.URL.Query().Get("preset")
	}
	if presetName == "" {
		presetName = "default"
	}

	p.forwardRequest(w, r, presetName)
}

// forwardRequest is the core proxying logic shared by handleMessages and handlePreset.
func (p *Proxy) forwardRequest(w http.ResponseWriter, r *http.Request, presetName string) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Extract session from query param or header
	session := ""
	if q := r.URL.Query().Get("session"); q != "" {
		session = q
	} else {
		session = r.Header.Get("X-Claudebar-Session")
	}
	safeSession := sanitizeSessionName(session)

	// Parse Anthropic request
	var anthropicReq AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		http.Error(w, fmt.Sprintf("invalid Anthropic request: %v", err), http.StatusBadRequest)
		return
	}

	// Look up the preset config
	p.presetsMu.RLock()
	cfg, exists := p.presets[presetName]
	p.presetsMu.RUnlock()
	if !exists {
		// Try "default"
		p.presetsMu.RLock()
		cfg, exists = p.presets["default"]
		p.presetsMu.RUnlock()
		if !exists {
			http.Error(w, fmt.Sprintf("unknown preset: %s", presetName), http.StatusNotFound)
			return
		}
	}

	// Convert to OpenAI format
	openaiReq, err := AnthropicToOpenAI(anthropicReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("transformation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Route to correct model based on slot
	modelSlot := openaiReq.Model
	actualModel, slotExists := cfg.Models[modelSlot]
	if !slotExists && modelSlot == "" {
		actualModel = cfg.Models["default"]
	}
	if !slotExists && modelSlot != "" {
		// Slot not found — fall back to default
		actualModel = cfg.Models["default"]
	}
	if actualModel != "" {
		openaiReq.Model = actualModel
	}

	reqID := generateReqID()
	anthropicBetaHeader := r.Header.Get("anthropic-beta")

	if anthropicReq.Stream {
		p.handleStreaming(w, r, openaiReq, cfg, anthropicBetaHeader, session, safeSession, reqID)
		return
	}

	openaiResp, err := p.forwardToOpenRouter(openaiReq, cfg, anthropicBetaHeader)
	if err != nil {
		http.Error(w, fmt.Sprintf("OpenRouter request failed: %v", err), http.StatusBadGateway)
		return
	}

	if openaiResp.Usage != nil {
		p.logUsage(session, openaiResp)
	}

	anthropicResp := OpenAIResponseToAnthropic(*openaiResp)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicResp)
}

// handleStreaming forwards a streaming request to OpenRouter and relays SSE chunks to the client.
func (p *Proxy) handleStreaming(w http.ResponseWriter, r *http.Request, openaiReq OpenAIRequest, cfg ProxyConfig, anthropicBetaHeader string, session, safeSession, reqID string) {
	stream, err := p.forwardToOpenRouterStream(openaiReq, cfg, anthropicBetaHeader)
	if err != nil {
		http.Error(w, fmt.Sprintf("streaming request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, _ := w.(http.Flusher)

	var streamBuf bytes.Buffer
	finalUsage, err := ForwardSSEStream(&streamBuf, stream, reqID, openaiReq.Model)
	if err != nil {
		// Partial stream: forward whatever we managed to read
		w.Write(streamBuf.Bytes())
		return
	}

	// Write the translated stream to the client
	w.Write(streamBuf.Bytes())
	if flusher != nil {
		flusher.Flush()
	}

	// Log usage from final chunk if available
	if finalUsage != nil {
		resp := &OpenRouterResponse{
			ID:    reqID,
			Model: openaiReq.Model,
			Usage: finalUsage,
		}
		p.logUsage(session, resp)
	}
}

// generateReqID creates a unique request ID
func generateReqID() string {
	return fmt.Sprintf("or-%d", time.Now().UnixNano())
}

// sanitizeSessionName replaces unsafe characters. If the result is empty, returns "unknown".
func sanitizeSessionName(name string) string {
	// If already safe, use as-is
	if name != "" && safeSessionRe.MatchString(name) {
		return name
	}

	// Escape directory traversal and shell metacharacters
	replaced := strings.ReplaceAll(name, "..", "")
	replaced = strings.ReplaceAll(replaced, "/", "")
	replaced = strings.ReplaceAll(replaced, "\\", "")
	replaced = strings.ReplaceAll(replaced, "\x00", "")

	if replaced == "" {
		return "unknown"
	}

	// For partially-safe names, hex-encode
	if !safeSessionRe.MatchString(replaced) {
		return fmt.Sprintf("sess-%x", []byte(replaced)[:min(len(replaced), 16)])
	}

	return replaced
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// forwardToOpenRouter sends a non-streaming request to OpenRouter and parses the response
func (p *Proxy) forwardToOpenRouter(openaiReq OpenAIRequest, cfg ProxyConfig, anthropicBetaHeader string) (*OpenRouterResponse, error) {
	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", cfg.BaseURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	// HTTP-Referer and X-Title are required by OpenRouter for display
	req.Header.Set("HTTP-Referer", "https://github.com/LabLeaks/claudebar")
	req.Header.Set("X-Title", "claudebar")
	if anthropicBetaHeader != "" {
		req.Header.Set("anthropic-beta", anthropicBetaHeader)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("OpenRouter error (%d): %s", resp.StatusCode, cleanErrorBody(resp.StatusCode, respBody))
	}

	var openrouterResp OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&openrouterResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &openrouterResp, nil
}

// forwardToOpenRouterStream sends a streaming request and returns the response body stream
func (p *Proxy) forwardToOpenRouterStream(openaiReq OpenAIRequest, cfg ProxyConfig, anthropicBetaHeader string) (io.ReadCloser, error) {
	openaiReq.Stream = true
	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling stream request: %w", err)
	}

	req, err := http.NewRequest("POST", cfg.BaseURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating stream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	req.Header.Set("Accept", "text/event-stream")
	if anthropicBetaHeader != "" {
		req.Header.Set("anthropic-beta", anthropicBetaHeader)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("OpenRouter stream error (%d): %s", resp.StatusCode, cleanErrorBody(resp.StatusCode, respBody))
	}

	return resp.Body, nil
}

// cleanErrorBody extracts a human-readable message from an error response body.
// If the body is HTML (e.g. Cloudflare error pages), returns a short summary.
// If JSON with an error.message field, returns just that message.
// Otherwise returns the raw body (truncated).
func cleanErrorBody(statusCode int, body []byte) string {
	s := strings.TrimSpace(string(body))

	// HTML error page (Cloudflare, nginx, etc) — don't dump the markup
	if strings.HasPrefix(s, "<!DOCTYPE") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<HTML") {
		// Try to extract <title> content
		re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
		return fmt.Sprintf("upstream returned HTML error page (HTTP %d)", statusCode)
	}

	// JSON error — extract the message field
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}

	// Fallback: return raw body, capped at a reasonable length
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// logUsage logs usage data for a session with LRU eviction
func (p *Proxy) logUsage(session string, resp *OpenRouterResponse) {
	if resp == nil || resp.Usage == nil {
		return
	}

	u := resp.Usage
	logEntry := UsageLogEntry{
		Timestamp:    time.Now().Format(time.RFC3339),
		Session:      session,
		Model:        resp.Model,
		PromptTokens: u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
		CostUSD:      u.Cost,
		RequestID:    resp.ID,
	}

	if u.PromptTokensDetails != nil {
		logEntry.CachedTokens = u.PromptTokensDetails.CachedTokens
	}

	p.usageMutex.Lock()

	// Persist existing entries to disk before evicting (LRU)
	if len(p.usageLogs[session]) >= maxUsageLogEntries {
		p.flushAndEvictLocked(session)
	}

	p.usageLogs[session] = append(p.usageLogs[session], logEntry)
	p.usageMutex.Unlock()

	// Write to disk outside the lock
	p.writeUsageLog(session, logEntry)
}

// flushAndEvictLocked must be called with p.mu held. Flushes oldest half of entries
// for a session to disk, then discards them to cap memory.
func (p *Proxy) flushAndEvictLocked(session string) {
	logs := p.usageLogs[session]
	evictCount := len(logs) / 2
	if evictCount < 1 {
		evictCount = 1
	}

	entries := logs[:evictCount]
	p.writeUsageLogBatch(session, entries)
	p.usageLogs[session] = append([]UsageLogEntry{}, logs[evictCount:]...)
}

// writeUsageLogBatch writes multiple log entries to the session file. Caller need NOT hold mu.
func (p *Proxy) writeUsageLogBatch(session string, entries []UsageLogEntry) {
	safeName := sanitizeSessionName(session)
	logFile := filepath.Join(p.logDir, fmt.Sprintf("%s.jsonl", safeName))

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	var buf bytes.Buffer
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	f.Write(buf.Bytes())
}

// writeUsageLog writes a single usage log entry to a session-specific file.
// Session name is sanitized for path safety.
func (p *Proxy) writeUsageLog(session string, entry UsageLogEntry) {
	safeName := sanitizeSessionName(session)
	logFile := filepath.Join(p.logDir, fmt.Sprintf("%s.jsonl", safeName))

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()
}

// getUsage returns usage statistics for a session
func (p *Proxy) getUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session from path
	session := strings.TrimPrefix(r.URL.Path, "/usage/")
	if session == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}

	// Copy data under lock, release before encoding
	var logs []UsageLogEntry
	p.usageMutex.RLock()
	sessionLogs, exists := p.usageLogs[session]
	if !exists {
		p.usageMutex.RUnlock()
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	logs = make([]UsageLogEntry, len(sessionLogs))
	copy(logs, sessionLogs)
	p.usageMutex.RUnlock()

	// Calculate totals outside the lock
	var totalTokens, totalCachedTokens int
	var totalCost float64

	for _, log := range logs {
		totalTokens += log.TotalTokens
		totalCachedTokens += log.CachedTokens
		totalCost += log.CostUSD
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session":         session,
		"total_tokens":    totalTokens,
		"cached_tokens":   totalCachedTokens,
		"total_cost_usd":  totalCost,
		"request_count":   len(logs),
		"recent_requests": logs,
	})
}

// GetSessionUsage returns the current usage totals for a session
func (p *Proxy) GetSessionUsage(session string) (totalTokens int, totalCachedTokens int, totalCost float64) {
	var logs []UsageLogEntry

	p.usageMutex.RLock()
	sessionLogs, exists := p.usageLogs[session]
	if exists {
		logs = make([]UsageLogEntry, len(sessionLogs))
		copy(logs, sessionLogs)
	}
	p.usageMutex.RUnlock()

	if !exists {
		return 0, 0, 0
	}

	for _, log := range logs {
		totalTokens += log.TotalTokens
		totalCachedTokens += log.CachedTokens
		totalCost += log.CostUSD
	}

	return
}

// FlushAllUsage writes all in-memory usage logs to disk and clears the cache.
// Call before shutdown.
func (p *Proxy) FlushAllUsage() {
	p.usageMutex.Lock()
	for session, logs := range p.usageLogs {
		if len(logs) > 0 {
			safeName := sanitizeSessionName(session)
			logFile := filepath.Join(p.logDir, fmt.Sprintf("%s.jsonl", safeName))

			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				continue
			}

			for _, entry := range logs {
				data, merr := json.Marshal(entry)
				if merr != nil {
					continue
				}
				f.Write(append(data, '\n'))
			}
			f.Close()
		}
	}
	p.usageLogs = make(map[string][]UsageLogEntry)
	p.usageMutex.Unlock()
}

// GetSessions returns a sorted list of all tracked session names.
func (p *Proxy) GetSessions() []string {
	p.usageMutex.RLock()
	defer p.usageMutex.RUnlock()

	sessions := make([]string, 0, len(p.usageLogs))
	for s := range p.usageLogs {
		sessions = append(sessions, s)
	}
	sort.Strings(sessions)
	return sessions
}
