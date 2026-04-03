package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// routerConfig defines a named router configuration in claudebar's config.json.
// Each config maps to a CCR preset with provider, credentials, model slots, and transformers.
type routerConfig struct {
	Provider     string            `json:"provider"`
	APIKey       string            `json:"api_key"`
	Models       map[string]string `json:"models"`
	Transformers []interface{}     `json:"transformers,omitempty"`
}

// knownProviders maps provider short names to their API base URLs.
var knownProviders = map[string]string{
	"openrouter": "https://openrouter.ai/api/v1",
}

// routerEnvVars returns the env vars to inject into a tmux session when routing
// through CCR. Returns nil when routerName is empty (no router active).
func routerEnvVars(routerName string) []string {
	if routerName == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:3456/preset/%s/v1/messages", routerName),
		"ANTHROPIC_AUTH_TOKEN=claudebar",
		"ANTHROPIC_API_KEY=",
		"DISABLE_PROMPT_CACHING=1",
		"DISABLE_COST_WARNINGS=1",
		"NO_PROXY=127.0.0.1",
		"ENABLE_TOOL_SEARCH=true",
	}
}

// extractRouterFlag parses --router=<name> or --router <name> from args.
// Returns the router name and the remaining args with the flag removed.
func extractRouterFlag(args []string) (string, []string) {
	var router string
	var remaining []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--router=") {
			router = strings.TrimPrefix(args[i], "--router=")
		} else if args[i] == "--router" && i+1 < len(args) {
			router = args[i+1]
			i++ // skip next arg
		} else {
			remaining = append(remaining, args[i])
		}
	}
	return router, remaining
}

// validateRouterConfig checks that a router config has valid provider, key, and models.
func validateRouterConfig(name string, rc *routerConfig) error {
	if rc.Provider == "" {
		return fmt.Errorf("router config %q: missing provider", name)
	}
	if _, ok := knownProviders[rc.Provider]; !ok {
		known := make([]string, 0, len(knownProviders))
		for k := range knownProviders {
			known = append(known, k)
		}
		return fmt.Errorf("router config %q: unknown provider %q (known: %s)", name, rc.Provider, strings.Join(known, ", "))
	}
	if rc.APIKey == "" {
		return fmt.Errorf("router config %q: missing api_key", name)
	}
	// If key is an env var reference, check it's set
	if strings.HasPrefix(rc.APIKey, "$") {
		envName := rc.APIKey[1:]
		if os.Getenv(envName) == "" {
			return fmt.Errorf("router config %q: api_key references $%s but it is not set", name, envName)
		}
	}
	if len(rc.Models) == 0 {
		return fmt.Errorf("router config %q: missing models (at least 'default' slot required)", name)
	}
	if _, ok := rc.Models["default"]; !ok {
		return fmt.Errorf("router config %q: models must include 'default' slot", name)
	}
	return nil
}

func ccrConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude-code-router")
}

func ccrPidFile() string {
	return filepath.Join(configDir(), "ccr.pid")
}

// ccrRunning checks if a CCR process is alive by reading the PID file and sending signal 0.
func ccrRunning() (int, bool) {
	data, err := os.ReadFile(ccrPidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}
	// Signal 0 checks if process exists without affecting it
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(ccrPidFile())
		return pid, false
	}
	return pid, true
}

// generateCCRConfig writes CCR's config.json and preset files from claudebar's router configs.
// Only called when CCR is not running.
func generateCCRConfig(cfg *globalConfig) error {
	if len(cfg.RouterConfigs) == 0 {
		return fmt.Errorf("no router configs defined")
	}

	dir := ccrConfigDir()
	if dir == "" {
		return fmt.Errorf("cannot determine CCR config directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating CCR config dir: %w", err)
	}
	presetsDir := filepath.Join(dir, "presets")
	if err := os.MkdirAll(presetsDir, 0755); err != nil {
		return fmt.Errorf("creating CCR presets dir: %w", err)
	}

	// Collect unique providers across all router configs.
	// CCR field names (from source): name, api_base_url, api_key, models, transformer.use
	type ccrTransformer struct {
		Use []interface{} `json:"use"`
	}
	type ccrProvider struct {
		Name        string         `json:"name"`
		APIBaseURL  string         `json:"api_base_url"`
		APIKey      string         `json:"api_key"`
		Models      []string       `json:"models,omitempty"`
		Transformer *ccrTransformer `json:"transformer,omitempty"`
	}

	providersSeen := make(map[string]bool)
	var providers []ccrProvider

	for _, rc := range cfg.RouterConfigs {
		if providersSeen[rc.Provider] {
			continue
		}
		providersSeen[rc.Provider] = true

		baseURL, _ := knownProviders[rc.Provider]

		// Collect all unique model names across all configs using this provider
		modelSet := make(map[string]bool)
		for _, rcInner := range cfg.RouterConfigs {
			if rcInner.Provider == rc.Provider {
				for _, modelSlot := range rcInner.Models {
					// Model slot format: "provider,model" — extract model part
					parts := strings.SplitN(modelSlot, ",", 2)
					if len(parts) == 2 {
						modelSet[parts[1]] = true
					}
				}
			}
		}
		var models []string
		for m := range modelSet {
			models = append(models, m)
		}

		p := ccrProvider{
			Name:       rc.Provider,
			APIBaseURL: baseURL,
			APIKey:     rc.APIKey,
			Models:     models,
		}
		if len(rc.Transformers) > 0 {
			p.Transformer = &ccrTransformer{Use: rc.Transformers}
		}
		providers = append(providers, p)
	}

	// Write main CCR config.json with CCR's expected top-level field names
	ccrConfig := map[string]interface{}{
		"PORT":      3456,
		"HOST":      "127.0.0.1",
		"APIKEY":    "",
		"Providers": providers,
	}

	configData, err := json.MarshalIndent(ccrConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling CCR config: %w", err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, configData, 0600); err != nil {
		return fmt.Errorf("writing CCR config: %w", err)
	}

	// Write preset files — one per router config.
	// CCR preset structure: { config: { Router: { slot: "provider,model" } } }
	for name, rc := range cfg.RouterConfigs {
		preset := map[string]interface{}{
			"config": map[string]interface{}{
				"Router": rc.Models,
			},
		}
		presetData, err := json.MarshalIndent(preset, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling preset %q: %w", name, err)
		}
		presetPath := filepath.Join(presetsDir, name+".json")
		if err := os.WriteFile(presetPath, presetData, 0600); err != nil {
			return fmt.Errorf("writing preset %q: %w", name, err)
		}
	}

	return nil
}

// ensureCCRRunning starts CCR if it's not already running.
func ensureCCRRunning(cfg *globalConfig) error {
	if _, alive := ccrRunning(); alive {
		return nil
	}

	// Check ccr is installed
	ccrPath, err := exec.LookPath("ccr")
	if err != nil {
		return fmt.Errorf("ccr not found in PATH — install with: npm install -g @musistudio/claude-code-router")
	}

	// Generate config + presets
	if err := generateCCRConfig(cfg); err != nil {
		return fmt.Errorf("generating CCR config: %w", err)
	}

	// Spawn ccr start as detached background process
	cmd := exec.Command(ccrPath, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// Detach from parent process group
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting CCR: %w", err)
	}

	// Write PID — kill the process if we can't record it, to avoid orphaning
	pid := cmd.Process.Pid
	if err := os.WriteFile(ccrPidFile(), []byte(strconv.Itoa(pid)), 0600); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("writing CCR PID file: %w", err)
	}

	// Poll for port 3456 to accept connections (up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:3456", 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("CCR started (pid %d) but port 3456 not ready after 5s", pid)
}

// stopCCR kills the CCR process and removes the PID file.
func stopCCR() {
	pid, alive := ccrRunning()
	if !alive {
		// Clean up stale PID file
		os.Remove(ccrPidFile())
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		proc.Signal(syscall.SIGTERM)
		// Give it a moment to shut down
		time.Sleep(500 * time.Millisecond)
		// Force kill if still alive
		if _, still := ccrRunning(); still {
			proc.Kill()
		}
	}
	os.Remove(ccrPidFile())
}

// countRoutedSessions counts live tmux sessions that have an active router.
func countRoutedSessions() int {
	live := liveTmuxSessionsFunc()
	count := 0
	entries, err := os.ReadDir(stateDir())
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".state.json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".state.json")
		if !live[name] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stateDir(), e.Name()))
		if err != nil {
			continue
		}
		var s claudeSessionState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.Router != "" {
			count++
		}
	}
	return count
}

// routerMenuItems returns tmux display-menu args for the router section.
func routerMenuItems(self, sess string, state *claudeSessionState, cfg *globalConfig) []string {
	if len(cfg.RouterConfigs) == 0 {
		return nil
	}

	var args []string
	args = append(args, "") // separator before section
	args = append(args, "#[fg=#888888]Router", "", "")

	// Sort config names for deterministic menu order
	names := make([]string, 0, len(cfg.RouterConfigs))
	for name := range cfg.RouterConfigs {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		sessionOn := state.Router == name
		configOn := cfg.Router == name
		label := fmt.Sprintf("%s  %s", featureState(sessionOn, configOn), name)
		key := strconv.Itoa(i + 1)
		args = append(args, label, key,
			fmt.Sprintf("run-shell '%s _toggle_router %s'", self, name))
	}

	return args
}

// runToggleRouter implements the radio-button cycle for router configs.
// OFF → ON → ALWAYS → OFF. Only one router active at a time.
func runToggleRouter(configName string) {
	sess := currentSession()
	state := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}
	cfg := loadConfig()

	// Validate config exists
	if _, ok := cfg.RouterConfigs[configName]; !ok {
		tmuxExec("display-message", fmt.Sprintf("Router config %q not found", configName))
		return
	}

	// Current state for this config
	sessionOn := state.Router == configName
	configOn := cfg.Router == configName

	newSession, newConfig := cycleFeature(sessionOn, configOn)

	if newSession {
		state.Router = configName
	} else {
		state.Router = ""
	}
	if newConfig {
		cfg.Router = configName
	} else {
		if cfg.Router == configName {
			cfg.Router = ""
		}
	}

	saveConfig(cfg)

	newState := featureState(state.Router == configName, cfg.Router == configName)

	// Check if session state actually changed
	old := loadState(sess)
	sessionChanged := old.Router != state.Router

	if sessionChanged {
		if state.Router != "" {
			// Activating — ensure CCR is running
			if err := ensureCCRRunning(cfg); err != nil {
				tmuxExec("display-message", fmt.Sprintf("Router error: %v", err))
				return
			}
		}
		tmuxExec("display-message", fmt.Sprintf("%s → %s (restarting...)", configName, newState))
		restartClaudeWithResume(sess, state)

		// If deactivating and no other routed sessions, stop CCR
		if state.Router == "" && countRoutedSessions() == 0 {
			stopCCR()
		}
	} else {
		tmuxExec("display-message", fmt.Sprintf("%s → %s", configName, newState))
		saveState(sess, state)
	}
}

// cleanupOrphanedCCR stops CCR if it's running but no sessions use it.
// Called on claudebar startup to handle cases like kill-session leaving CCR orphaned.
func cleanupOrphanedCCR() {
	if _, alive := ccrRunning(); !alive {
		return
	}
	if countRoutedSessions() == 0 {
		stopCCR()
	}
}
