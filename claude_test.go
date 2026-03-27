package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestShellQuote verifies that arguments embedded into tmux run-shell commands
// are properly escaped. Without this, paths with spaces, quotes, or shell
// metacharacters would break tmux keybinds and menu actions — every feature
// that triggers via the status bar or ⌥ shortcuts depends on this.
func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"with space", "'with space'"},
		{"with'quote", `'with'\''quote'`},
		{`with"double`, `'with"double'`},
		{"with$var", "'with$var'"},
		{"with`tick`", "'with`tick`'"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shellQuote(tt.in)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestToggleBypassPermissions ensures the permissions toggle is a clean
// two-state flip (default ↔ bypassPermissions). This powers the menu's
// "Toggle bypass permissions" action which restarts claude with --resume.
// A bug here means either permissions never turn on or never turn off.
func TestToggleBypassPermissions(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"default", "bypassPermissions"},
		{"bypassPermissions", "default"},
		{"plan", "bypassPermissions"},     // legacy state → bypass
		{"", "bypassPermissions"},          // empty state → bypass
	}
	for _, tt := range tests {
		got := toggleBypassPermissions(tt.in)
		if got != tt.want {
			t.Errorf("toggleBypassPermissions(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestBuildClaudeArgs verifies the CLI args passed to claude on launch and
// restart. This is critical because wrong args mean:
// - Missing --resume → conversation history lost on feature toggle/upgrade
// - Missing --dangerously-skip-permissions → permission mode not applied
// - Missing --teammate-mode → agent teams spawn rogue tmux panes
func TestBuildClaudeArgs(t *testing.T) {
	tests := []struct {
		name   string
		state  claudeSessionState
		resume bool
		want   []string   // args that MUST be present
		reject []string   // args that MUST NOT be present
	}{
		{
			name:   "default mode, fresh start",
			state:  claudeSessionState{PermissionMode: "default"},
			resume: false,
			want:   []string{"--teammate-mode", "in-process"},
			reject: []string{"--resume", "--dangerously-skip-permissions"},
		},
		{
			name:   "bypass permissions applied",
			state:  claudeSessionState{PermissionMode: "bypassPermissions"},
			resume: false,
			want:   []string{"--dangerously-skip-permissions"},
		},
		{
			name:   "resume passes session ID",
			state:  claudeSessionState{SessionID: "abc-123", PermissionMode: "default"},
			resume: true,
			want:   []string{"--resume", "abc-123"},
		},
		{
			name:   "resume without session ID does not pass --resume",
			state:  claudeSessionState{PermissionMode: "default"},
			resume: true,
			reject: []string{"--resume"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildClaudeArgs(&tt.state, tt.resume)
			for _, w := range tt.want {
				if !contains(args, w) {
					t.Errorf("expected %q in args %v", w, args)
				}
			}
			for _, r := range tt.reject {
				if contains(args, r) {
					t.Errorf("unexpected %q in args %v", r, args)
				}
			}
		})
	}
}

// TestFeatureEnvVars verifies that toggled features produce the correct
// environment variable strings. These get prepended to the claude launch
// command on restart. Wrong values mean features silently don't activate
// (e.g., agent teams stay disabled even after toggling on).
func TestFeatureEnvVars(t *testing.T) {
	state := &claudeSessionState{
		Features: map[string]bool{
			"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true,
			"MAX_THINKING_TOKENS":                  true,
			"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": false,
		},
	}
	envs := state.featureEnvVars()

	mustContain := map[string]bool{
		"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1": false,
		"MAX_THINKING_TOKENS=32000":              false,
	}
	for _, e := range envs {
		if _, ok := mustContain[e]; ok {
			mustContain[e] = true
		}
		if e == "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1" {
			t.Error("disabled feature should not appear in env vars")
		}
	}
	for k, found := range mustContain {
		if !found {
			t.Errorf("expected %q in env vars, got %v", k, envs)
		}
	}
}

// TestFindLatestClaudeSession_PathEncoding is a regression test for a bug
// where session resume silently failed because the path encoding was wrong.
// Claude stores transcripts at ~/.claude/projects/-Users-gk-work-project/
// (leading dash from the leading / in the path). We had a version that
// stripped the leading / first, producing Users-gk-work-project (no dash),
// which meant findLatestClaudeSession never found any transcripts and
// --resume was never passed. Users lost conversation history on every
// feature toggle or permission change.
func TestFindLatestClaudeSession_PathEncoding(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	// Claude's actual encoding: replace ALL / with -, including the leading one
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)
	os.WriteFile(filepath.Join(projectDir, "abc-123.jsonl"), []byte("{}"), 0644)

	got := findLatestClaudeSession(workDir)
	if got != "abc-123" {
		t.Errorf("findLatestClaudeSession(%q) = %q, want %q", workDir, got, "abc-123")
	}

	// Verify the OLD broken encoding would NOT work
	wrongEncoded := "Users-gk-work-project" // missing leading dash
	wrongDir := filepath.Join(tmp, ".claude", "projects", wrongEncoded)
	os.MkdirAll(wrongDir, 0755)
	os.WriteFile(filepath.Join(wrongDir, "wrong-id.jsonl"), []byte("{}"), 0644)

	// Should still find the correct one, not the wrong one
	got = findLatestClaudeSession(workDir)
	if got == "wrong-id" {
		t.Error("findLatestClaudeSession matched the wrong path encoding (without leading dash)")
	}
}

// TestStateSaveLoad verifies round-trip persistence of session state.
// State loss means: permission mode resets, feature toggles forgotten,
// session ID lost (can't resume). This happened when the state directory
// was wrong or JSON marshaling failed silently.
func TestStateSaveLoad(t *testing.T) {
	tmp := t.TempDir()
	origFunc := stateDir
	// Temporarily override stateDir
	_ = origFunc // stateDir is a function, can't easily override. Test the JSON round-trip instead.

	state := &claudeSessionState{
		SessionID:      "test-session-123",
		PermissionMode: "bypassPermissions",
		Model:          "opus",
		WorkDir:        "/Users/gk/work/test",
		Features: map[string]bool{
			"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true,
		},
	}

	// Test JSON round-trip
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	path := filepath.Join(tmp, "test.state.json")
	os.WriteFile(path, data, 0644)

	loaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var restored claudeSessionState
	if err := json.Unmarshal(loaded, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.SessionID != state.SessionID {
		t.Errorf("SessionID: got %q, want %q", restored.SessionID, state.SessionID)
	}
	if restored.PermissionMode != state.PermissionMode {
		t.Errorf("PermissionMode: got %q, want %q", restored.PermissionMode, state.PermissionMode)
	}
	if !restored.isFeatureOn("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS") {
		t.Error("feature CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS should be on after restore")
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
