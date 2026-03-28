package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// claudeSessionState tracks the current claude session for restart+resume operations
type claudeSessionState struct {
	SessionID      string            `json:"session_id"`
	PermissionMode string            `json:"permission_mode"` // "plan", "default", "bypassPermissions"
	Model          string            `json:"model"`
	WorkDir        string            `json:"work_dir"`
	Features       map[string]bool   `json:"features,omitempty"` // toggleable env var features
}

type feature struct {
	Label    string
	OnValue  string // value when enabled
}

// Feature definitions: env var name → feature config
var featureRegistry = map[string]feature{
	"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": {Label: "Agent Teams", OnValue: "1"},
	"MAX_THINKING_TOKENS":                  {Label: "Max Thinking (32k)", OnValue: "32000"},
	"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": {Label: "Disable Background Tasks", OnValue: "1"},
}

// Ordered keys for consistent menu display
var featureOrder = []string{
	"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS",
	"MAX_THINKING_TOKENS",
	"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS",
}

func (s *claudeSessionState) isFeatureOn(envVar string) bool {
	if s.Features == nil {
		return false
	}
	return s.Features[envVar]
}

func (s *claudeSessionState) toggleFeature(envVar string) {
	if s.Features == nil {
		s.Features = make(map[string]bool)
	}
	s.Features[envVar] = !s.Features[envVar]
}

func (s *claudeSessionState) featureEnvVars() []string {
	var envs []string
	for k, on := range s.Features {
		if on {
			if f, ok := featureRegistry[k]; ok {
				envs = append(envs, k+"="+f.OnValue)
			} else {
				envs = append(envs, k+"=1")
			}
		}
	}
	return envs
}

// globalConfig holds user preferences that apply to all new sessions
type globalConfig struct {
	PermissionMode string          `json:"permission_mode,omitempty"` // default permission mode for new sessions
	Features       map[string]bool `json:"features,omitempty"`        // features enabled by default
	Model          string          `json:"model,omitempty"`           // default model
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	d := filepath.Join(home, ".config", "claudebar")
	os.MkdirAll(d, 0755)
	return d
}

func configFile() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() *globalConfig {
	data, err := os.ReadFile(configFile())
	if err != nil {
		return &globalConfig{}
	}
	var cfg globalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &globalConfig{}
	}
	return &cfg
}

func saveConfig(cfg *globalConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile(), data, 0644)
}

func stateDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	d := filepath.Join(dir, "claudebar")
	os.MkdirAll(d, 0755)
	return d
}

func stateFile(tmuxSession string) string {
	return filepath.Join(stateDir(), tmuxSession+".state.json")
}

func loadState(tmuxSession string) (*claudeSessionState, error) {
	path := stateFile(tmuxSession)
	data, err := os.ReadFile(path)
	if err != nil {
		return &claudeSessionState{PermissionMode: "default"}, nil
	}
	var s claudeSessionState
	if err := json.Unmarshal(data, &s); err != nil {
		snippet := string(data)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		fmt.Fprintf(os.Stderr, "claudebar: corrupt state file %s: %v\nContent: %s\n", path, err, snippet)
		return &claudeSessionState{PermissionMode: "default"}, nil
	}
	return &s, nil
}

func saveState(tmuxSession string, s *claudeSessionState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile(tmuxSession), data, 0644)
}

func buildClaudeArgs(tmuxSession string, state *claudeSessionState, resume bool) []string {
	args := []string{}

	if resume && state.SessionID != "" {
		args = append(args, "--resume", state.SessionID)
	}

	switch state.PermissionMode {
	case "bypassPermissions":
		args = append(args, "--dangerously-skip-permissions")
	case "plan":
		args = append(args, "--permission-mode", "plan")
	}

	if state.Model != "" {
		args = append(args, "--model", state.Model)
	}

	// Force in-process teammate mode since claudebar manages its own tmux layout
	args = append(args, "--teammate-mode", "in-process")

	// Set claudebar as the statusline handler to capture usage data
	self := selfPath()
	settingsFile := writeStatuslineSettings(self, tmuxSession)
	if settingsFile != "" {
		args = append(args, "--settings", settingsFile)
	}

	return args
}

func claudeBinary() string {
	// Find claude in PATH
	path, err := exec.LookPath("claude")
	if err != nil {
		return "claude"
	}
	return path
}

func launchClaude(tmuxSession string, state *claudeSessionState, resume bool) string {
	return launchClaudeWithExtra(tmuxSession, state, resume, nil)
}

func launchClaudeWithExtra(tmuxSession string, state *claudeSessionState, resume bool, extraArgs []string) string {
	bin := claudeBinary()
	args := buildClaudeArgs(tmuxSession, state, resume)
	// Append any passthrough flags from the user
	args = append(args, extraArgs...)
	parts := []string{shellQuote(bin)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps a string in single quotes if it contains spaces or special chars
func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t'\"\\$`!") {
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	return s
}

// toggleBypassPermissions flips between default and bypassPermissions
func toggleBypassPermissions(current string) string {
	if current == "bypassPermissions" {
		return "default"
	}
	return "bypassPermissions"
}

// findLatestClaudeSession tries to extract the session ID from claude's output
// by reading the session file that claude writes
func findLatestClaudeSession(workDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	encoded := strings.ReplaceAll(workDir, "/", "-")
	projectDir := filepath.Join(home, ".claude", "projects", encoded)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	// Find the most recently modified .jsonl transcript — its name is the session ID
	var latest string
	var latestTime int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latest = strings.TrimSuffix(e.Name(), ".jsonl")
		}
	}
	return latest
}

// restartClaudeWithResume kills current claude and relaunches with --resume
func restartClaudeWithResume(tmuxSession string, state *claudeSessionState) error {
	// Find the latest session ID from claude's session storage
	sessionID := findLatestClaudeSession(state.WorkDir)
	if sessionID != "" {
		state.SessionID = sessionID
	}

	// Save updated state
	if err := saveState(tmuxSession, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	// Send Ctrl+C then Ctrl+D to gracefully stop claude
	tmuxExec("send-keys", "-t", tmuxSession, "C-c", "")
	tmuxExec("send-keys", "-t", tmuxSession, "C-d", "")

	// Build command with env vars prepended
	envPrefix := ""
	for _, env := range state.featureEnvVars() {
		envPrefix += env + " "
	}
	cmd := envPrefix + launchClaude(tmuxSession, state, true)
	// Target pane 0 explicitly (the claude pane)
	return tmuxExec("respawn-pane", "-k", "-t", tmuxSession+":0.0", cmd)
}
