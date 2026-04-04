package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// routerEnvVars — pure function, no filesystem
// ---------------------------------------------------------------------------

// TestRouterEnvVars_Active verifies that an active router name produces the
// full set of env vars Claude Code needs to route through the native proxy,
// with the preset URL containing the router name.
func TestRouterEnvVars_Active(t *testing.T) {
	rc := &routerConfig{Provider: "openrouter", APIKey: "test", Models: map[string]string{"default": "qwen/test"}}
	envs := routerEnvVars("foo", rc, "test-session")
	if envs == nil {
		t.Fatal("expected non-nil env vars for active router")
	}
	if len(envs) != 7 {
		t.Errorf("expected 7 env vars, got %d: %v", len(envs), envs)
	}

	// ANTHROPIC_BASE_URL must contain the preset path
	var foundBaseURL bool
	for _, e := range envs {
		if strings.HasPrefix(e, "ANTHROPIC_BASE_URL=") {
			foundBaseURL = true
			if !strings.Contains(e, "/preset/foo") {
				t.Errorf("ANTHROPIC_BASE_URL should contain /preset/foo, got %q", e)
			}
		}
	}
	if !foundBaseURL {
		t.Error("ANTHROPIC_BASE_URL not found in env vars")
	}

	// Check other required vars are present
	required := []string{
		"ANTHROPIC_AUTH_TOKEN=",
		"ANTHROPIC_API_KEY=",
		"DISABLE_PROMPT_CACHING=1",
		"DISABLE_COST_WARNINGS=1",
		"NO_PROXY=127.0.0.1",
		"ENABLE_TOOL_SEARCH=true",
	}
	for _, r := range required {
		found := false
		for _, e := range envs {
			if strings.Contains(e, r) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected env var containing %q, not found in %v", r, envs)
		}
	}
}

// TestRouterEnvVars_Empty verifies that an empty router name returns nil
// (no env vars injected — session uses Anthropic directly).
func TestRouterEnvVars_Empty(t *testing.T) {
	envs := routerEnvVars("", nil, "")
	if envs != nil {
		t.Errorf("expected nil for empty router name, got %v", envs)
	}
}

// ---------------------------------------------------------------------------
// extractRouterFlag — pure function, no filesystem
// ---------------------------------------------------------------------------

// TestExtractRouterFlag tests all forms of the --router flag parsing.
func TestExtractRouterFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantArgs []string
	}{
		{
			name:     "equals form",
			args:     []string{"--router=foo", "--verbose"},
			wantName: "foo",
			wantArgs: []string{"--verbose"},
		},
		{
			name:     "space form",
			args:     []string{"--router", "foo", "--verbose"},
			wantName: "foo",
			wantArgs: []string{"--verbose"},
		},
		{
			name:     "missing flag",
			args:     []string{"--verbose"},
			wantName: "",
			wantArgs: []string{"--verbose"},
		},
		{
			name:     "no args",
			args:     []string{},
			wantName: "",
			wantArgs: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, remaining := extractRouterFlag(tt.args)
			if name != tt.wantName {
				t.Errorf("name: got %q, want %q", name, tt.wantName)
			}
			if len(remaining) != len(tt.wantArgs) {
				t.Errorf("remaining args: got %v (len %d), want %v (len %d)",
					remaining, len(remaining), tt.wantArgs, len(tt.wantArgs))
				return
			}
			for i := range remaining {
				if remaining[i] != tt.wantArgs[i] {
					t.Errorf("remaining[%d]: got %q, want %q", i, remaining[i], tt.wantArgs[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateRouterConfig — pure function, no filesystem
// ---------------------------------------------------------------------------

func TestValidateRouterConfig(t *testing.T) {
	tests := []struct {
		name      string
		rcName    string
		rc        *routerConfig
		wantErr   string // substring that must appear in error, "" means no error
	}{
		{
			name:   "valid config",
			rcName: "test",
			rc: &routerConfig{
				Provider: "openrouter",
				APIKey:   "sk-literal-key",
				Models:   map[string]string{"default": "openrouter,qwen/qwen3.6-plus:free"},
			},
			wantErr: "",
		},
		{
			name:   "unknown provider",
			rcName: "test",
			rc: &routerConfig{
				Provider: "nonexistent-provider",
				APIKey:   "key123",
				Models:   map[string]string{"default": "p,m"},
			},
			wantErr: "unknown provider",
		},
		{
			name:   "no API key",
			rcName: "test",
			rc: &routerConfig{
				Provider: "openrouter",
				APIKey:   "",
				Models:   map[string]string{"default": "p,m"},
			},
			wantErr: "api_key",
		},
		{
			name:   "no default model",
			rcName: "test",
			rc: &routerConfig{
				Provider: "openrouter",
				APIKey:   "key123",
				Models:   map[string]string{"think": "p,m"},
			},
			wantErr: "default",
		},
		{
			name:   "empty models map",
			rcName: "test",
			rc: &routerConfig{
				Provider: "openrouter",
				APIKey:   "key123",
				Models:   nil,
			},
			wantErr: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRouterConfig(tt.rcName, tt.rc)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// (CCR tests removed — CCR replaced by native proxy in sprint 005)
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Config/state round-trip — filesystem tests
// ---------------------------------------------------------------------------

// TestConfigRoundTrip_WithRouterConfigs verifies that saving and loading a
// globalConfig with RouterConfigs preserves all fields.
func TestConfigRoundTrip_WithRouterConfigs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".config", "claudebar"), 0755)

	original := &globalConfig{
		PermissionMode: "default",
		Features:       map[string]bool{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true},
		Router:         "openrouter-qwen",
		RouterConfigs: map[string]*routerConfig{
			"openrouter-qwen": {
				Provider: "openrouter",
				APIKey:   "sk-test-key",
				Models:   map[string]string{"default": "openrouter,qwen/qwen3.6-plus:free", "think": "openrouter,qwen/qwen3.6-plus:free"},
			},
		},
	}

	if err := saveConfig(original); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	loaded := loadConfig()

	// Verify top-level fields
	if loaded.Router != original.Router {
		t.Errorf("Router: got %q, want %q", loaded.Router, original.Router)
	}
	if len(loaded.RouterConfigs) != 1 {
		t.Fatalf("RouterConfigs: got %d entries, want 1", len(loaded.RouterConfigs))
	}

	rc := loaded.RouterConfigs["openrouter-qwen"]
	if rc == nil {
		t.Fatal("RouterConfigs[\"openrouter-qwen\"] is nil")
	}
	if rc.Provider != "openrouter" {
		t.Errorf("Provider: got %q, want %q", rc.Provider, "openrouter")
	}
	if rc.APIKey != "sk-test-key" {
		t.Errorf("APIKey: got %q, want %q", rc.APIKey, "sk-test-key")
	}
	if rc.Models["default"] != "openrouter,qwen/qwen3.6-plus:free" {
		t.Errorf("Models[default]: got %q", rc.Models["default"])
	}
	if rc.Models["think"] != "openrouter,qwen/qwen3.6-plus:free" {
		t.Errorf("Models[think]: got %q", rc.Models["think"])
	}
}

// TestSaveConfig_Chmod600WithRouterConfigs verifies that config files containing
// router configs (which have API keys) are saved with mode 0600.
func TestSaveConfig_Chmod600WithRouterConfigs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".config", "claudebar"), 0755)

	cfg := &globalConfig{
		RouterConfigs: map[string]*routerConfig{
			"test": {
				Provider: "openrouter",
				APIKey:   "secret",
				Models:   map[string]string{"default": "p,m"},
			},
		},
	}

	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	info, err := os.Stat(configFile())
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("config with RouterConfigs should be 0600, got %o", info.Mode().Perm())
	}
}

// TestSaveConfig_Chmod644WithoutRouterConfigs verifies that config files without
// router configs are saved with the default mode 0644.
func TestSaveConfig_Chmod644WithoutRouterConfigs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".config", "claudebar"), 0755)

	cfg := &globalConfig{
		PermissionMode: "default",
		Features:       map[string]bool{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true},
	}

	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	info, err := os.Stat(configFile())
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("config without RouterConfigs should be 0644, got %o", info.Mode().Perm())
	}
}

// TestStateRoundTrip_WithRouter verifies that saving and loading session state
// with a Router field preserves it.
func TestStateRoundTrip_WithRouter(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".config", "claudebar"), 0755)

	original := &claudeSessionState{
		SessionID:      "sess-abc",
		PermissionMode: "default",
		Router:         "openrouter-qwen",
		Features:       map[string]bool{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true},
	}

	if err := saveState("test-session", original); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	loaded := loadState("test-session")
	if loaded.Router != "openrouter-qwen" {
		t.Errorf("Router: got %q, want %q", loaded.Router, "openrouter-qwen")
	}
	if loaded.SessionID != "sess-abc" {
		t.Errorf("SessionID: got %q, want %q", loaded.SessionID, "sess-abc")
	}
}

// ---------------------------------------------------------------------------
// Radio-button toggle — uses cycleFeature pattern
// ---------------------------------------------------------------------------

// TestRunToggleRouter_Cycle verifies that router activation follows the same
// OFF → ON → ALWAYS → OFF cycle as feature toggles.
func TestRunToggleRouter_Cycle(t *testing.T) {
	// cycleFeature is the underlying mechanism: (sessionOn, configOn) → (newSession, newConfig)
	// OFF → ON
	s, c := cycleFeature(false, false)
	if !s || c {
		t.Errorf("OFF→ON: got session=%v config=%v, want session=true config=false", s, c)
	}

	// ON → ALWAYS
	s, c = cycleFeature(true, false)
	if !s || !c {
		t.Errorf("ON→ALWAYS: got session=%v config=%v, want session=true config=true", s, c)
	}

	// ALWAYS → OFF
	s, c = cycleFeature(true, true)
	if s || c {
		t.Errorf("ALWAYS→OFF: got session=%v config=%v, want session=false config=false", s, c)
	}
}

// ---------------------------------------------------------------------------
// countRoutedSessions — filesystem test
// ---------------------------------------------------------------------------

// TestCountRoutedSessions_Mixed verifies that countRoutedSessions correctly
// counts only sessions with a non-empty Router field.
func TestCountRoutedSessions_Mixed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "routed-1", "routed-2", "plain")

	configBase := filepath.Join(tmp, ".config", "claudebar")
	os.MkdirAll(configBase, 0755)

	writeStateFile(t, configBase, "routed-1.state.json", &claudeSessionState{
		SessionID: "s1",
		Router:    "openrouter-qwen",
	})
	writeStateFile(t, configBase, "routed-2.state.json", &claudeSessionState{
		SessionID: "s2",
		Router:    "openrouter-deepseek",
	})
	writeStateFile(t, configBase, "plain.state.json", &claudeSessionState{
		SessionID: "s3",
	})

	count := countRoutedSessions()
	if count != 2 {
		t.Errorf("countRoutedSessions: got %d, want 2", count)
	}
}

// TestCountRoutedSessions_None verifies that countRoutedSessions returns 0
// when no sessions have a Router field set.
func TestCountRoutedSessions_None(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "plain-1")

	configBase := filepath.Join(tmp, ".config", "claudebar")
	os.MkdirAll(configBase, 0755)

	writeStateFile(t, configBase, "plain-1.state.json", &claudeSessionState{
		SessionID: "s1",
	})

	count := countRoutedSessions()
	if count != 0 {
		t.Errorf("countRoutedSessions: got %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// featureEnvVars independence — router state must not leak into features
// ---------------------------------------------------------------------------

// TestFeatureEnvVars_UnaffectedByRouter verifies that featureEnvVars only
// returns feature-related env vars, not router env vars, even when the
// session has an active router.
func TestFeatureEnvVars_UnaffectedByRouter(t *testing.T) {
	state := &claudeSessionState{
		Features: map[string]bool{
			"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true,
		},
		Router: "openrouter-qwen",
	}

	envs := state.featureEnvVars()

	for _, e := range envs {
		if strings.HasPrefix(e, "ANTHROPIC_BASE_URL=") ||
			strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") ||
			strings.HasPrefix(e, "DISABLE_PROMPT_CACHING=") ||
			strings.HasPrefix(e, "DISABLE_COST_WARNINGS=") {
			t.Errorf("featureEnvVars should not contain router env var %q", e)
		}
	}

	// Should still contain feature vars
	found := false
	for _, e := range envs {
		if e == "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 in %v", envs)
	}
}
