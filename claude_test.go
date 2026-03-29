package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			args := buildClaudeArgs("test-session", &tt.state, tt.resume)
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

	got := findLatestClaudeSession(workDir, nil)
	if got != "abc-123" {
		t.Errorf("findLatestClaudeSession(%q) = %q, want %q", workDir, got, "abc-123")
	}

	// Verify the OLD broken encoding would NOT work
	wrongEncoded := "Users-gk-work-project" // missing leading dash
	wrongDir := filepath.Join(tmp, ".claude", "projects", wrongEncoded)
	os.MkdirAll(wrongDir, 0755)
	os.WriteFile(filepath.Join(wrongDir, "wrong-id.jsonl"), []byte("{}"), 0644)

	// Should still find the correct one, not the wrong one
	got = findLatestClaudeSession(workDir, nil)
	if got == "wrong-id" {
		t.Error("findLatestClaudeSession matched the wrong path encoding (without leading dash)")
	}
}

// TestFindLatestClaudeSession_SkipsClaimedSessions verifies that
// findLatestClaudeSession respects the skip set, so it won't grab a session
// that another claudebar instance has already claimed.
func TestFindLatestClaudeSession_SkipsClaimedSessions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	now := time.Now()
	os.WriteFile(filepath.Join(projectDir, "claimed-newest.jsonl"), []byte("{}"), 0644)
	os.Chtimes(filepath.Join(projectDir, "claimed-newest.jsonl"), now, now)

	os.WriteFile(filepath.Join(projectDir, "unclaimed-older.jsonl"), []byte("{}"), 0644)
	os.Chtimes(filepath.Join(projectDir, "unclaimed-older.jsonl"), now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	skip := map[string]bool{"claimed-newest": true}
	got := findLatestClaudeSession(workDir, skip)
	if got != "unclaimed-older" {
		t.Errorf("expected 'unclaimed-older', got %q", got)
	}

	// When all are skipped, should return empty
	skip["unclaimed-older"] = true
	got = findLatestClaudeSession(workDir, skip)
	if got != "" {
		t.Errorf("expected empty string when all skipped, got %q", got)
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

func TestBuildClaudeArgsIncludesSettings(t *testing.T) {
	state := &claudeSessionState{PermissionMode: "default"}
	args := buildClaudeArgs("my-project", state, false)

	settingsIdx := -1
	for i, a := range args {
		if a == "--settings" {
			settingsIdx = i
			break
		}
	}
	if settingsIdx < 0 {
		t.Fatal("--settings not found in args")
	}
	if settingsIdx+1 >= len(args) {
		t.Fatal("--settings has no value")
	}
	settingsPath := args[settingsIdx+1]
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings file %s: %v", settingsPath, err)
	}
	if !strings.Contains(string(data), "my-project") {
		t.Errorf("settings file should contain session name 'my-project', got: %s", string(data))
	}
	os.Remove(settingsPath)
}

// --- Session differentiation tests ---

// TestClaimedSessionIDs_Empty verifies that claimedSessionIDs returns an empty
// map when no state files exist. This is the initial state before any claudebar
// session has been created.
func TestClaimedSessionIDs_Empty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// stateDir uses os.UserConfigDir which on macOS returns ~/Library/Application Support
	// On test: create the claudebar dir manually
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	claimed := claimedSessionIDs()
	if len(claimed) != 0 {
		t.Errorf("expected empty map, got %v", claimed)
	}
}

// TestClaimedSessionIDs_ReturnsSessionIDs verifies that claimedSessionIDs
// correctly extracts session IDs from valid state files. This is how claudebar
// knows which Claude sessions are already "owned" by a tmux session.
func TestClaimedSessionIDs_ReturnsSessionIDs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "project-a", "project-b")
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	// Write state files with session IDs
	writeStateFile(t, configBase, "project-a.state.json", &claudeSessionState{
		SessionID: "sess-111",
	})
	writeStateFile(t, configBase, "project-b.state.json", &claudeSessionState{
		SessionID: "sess-222",
	})

	claimed := claimedSessionIDs()
	if len(claimed) != 2 {
		t.Fatalf("expected 2 claimed sessions, got %d: %v", len(claimed), claimed)
	}
	if !claimed["sess-111"] {
		t.Error("expected sess-111 to be claimed")
	}
	if !claimed["sess-222"] {
		t.Error("expected sess-222 to be claimed")
	}
}

// TestClaimedSessionIDs_SkipsEmptySessionID verifies that state files with no
// session ID (e.g., freshly created sessions that haven't run claude yet) are
// not included in the claimed set.
func TestClaimedSessionIDs_SkipsEmptySessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "has-id", "no-id")
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	writeStateFile(t, configBase, "has-id.state.json", &claudeSessionState{
		SessionID: "sess-aaa",
	})
	writeStateFile(t, configBase, "no-id.state.json", &claudeSessionState{
		SessionID: "",
	})

	claimed := claimedSessionIDs()
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed session, got %d: %v", len(claimed), claimed)
	}
	if !claimed["sess-aaa"] {
		t.Error("expected sess-aaa to be claimed")
	}
}

// TestClaimedSessionIDs_HandlesCorruptStateFiles verifies that corrupt or
// unreadable state files are silently skipped rather than causing a crash.
// This matters because state files can be corrupted by partial writes during
// restarts or system crashes.
func TestClaimedSessionIDs_HandlesCorruptStateFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "good", "corrupt")
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	// Valid state file
	writeStateFile(t, configBase, "good.state.json", &claudeSessionState{
		SessionID: "sess-good",
	})
	// Corrupt state file (invalid JSON)
	os.WriteFile(filepath.Join(configBase, "corrupt.state.json"), []byte("{broken json!!!"), 0644)
	// Non-state file (should be ignored)
	os.WriteFile(filepath.Join(configBase, "readme.txt"), []byte("not a state file"), 0644)

	claimed := claimedSessionIDs()
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed session, got %d: %v", len(claimed), claimed)
	}
	if !claimed["sess-good"] {
		t.Error("expected sess-good to be claimed")
	}
}

// TestClaudeSessionExists_True verifies that claudeSessionExists returns true
// when a .jsonl transcript file exists for the given session ID. This check
// prevents restartClaudeWithResume from passing a stale --resume ID.
func TestClaudeSessionExists_True(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/myproject"
	encoded := "-Users-gk-work-myproject"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)
	os.WriteFile(filepath.Join(projectDir, "sess-123.jsonl"), []byte("{}"), 0644)

	if !claudeSessionExists(workDir, "sess-123") {
		t.Error("expected claudeSessionExists to return true for existing .jsonl")
	}
}

// TestClaudeSessionExists_False verifies that claudeSessionExists returns false
// when no .jsonl file exists for the session ID. This triggers a re-scan for
// the latest session during restart.
func TestClaudeSessionExists_False(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/myproject"
	encoded := "-Users-gk-work-myproject"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	if claudeSessionExists(workDir, "nonexistent-id") {
		t.Error("expected claudeSessionExists to return false for missing .jsonl")
	}
}

// TestClaudeSessionExists_MissingProjectDir verifies graceful handling when
// the project directory itself doesn't exist (e.g., brand new project that
// claude has never been run in).
func TestClaudeSessionExists_MissingProjectDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if claudeSessionExists("/nonexistent/path", "any-id") {
		t.Error("expected claudeSessionExists to return false for missing project dir")
	}
}

// TestFindUnclaimedSessions_ReturnsUnclaimed verifies that findUnclaimedSessions
// only returns sessions that are NOT referenced by any state file. This is the
// core differentiation logic — when multiple claudebar instances share a work
// directory, each must resume its own session, not steal another's.
func TestFindUnclaimedSessions_ReturnsUnclaimed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "tmux-a", "tmux-b")

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	// Create 3 session transcripts
	os.WriteFile(filepath.Join(projectDir, "claimed-1.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectDir, "claimed-2.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectDir, "unclaimed-3.jsonl"), []byte("{}"), 0644)

	// Claim 2 of them via state files
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)
	writeStateFile(t, configBase, "tmux-a.state.json", &claudeSessionState{
		SessionID: "claimed-1",
	})
	writeStateFile(t, configBase, "tmux-b.state.json", &claudeSessionState{
		SessionID: "claimed-2",
	})

	unclaimed := findUnclaimedSessions(workDir)
	if len(unclaimed) != 1 {
		t.Fatalf("expected 1 unclaimed session, got %d: %v", len(unclaimed), unclaimed)
	}
	if unclaimed[0].Name != "unclaimed-3" {
		t.Errorf("expected unclaimed session 'unclaimed-3', got %q", unclaimed[0].Name)
	}
	if unclaimed[0].Ago == "" {
		t.Error("expected Ago to be populated")
	}
}

// TestFindUnclaimedSessions_EmptyWhenAllClaimed verifies that an empty slice
// is returned when every .jsonl is claimed by a state file.
func TestFindUnclaimedSessions_EmptyWhenAllClaimed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	setLiveSessions(t, "tmux-x")

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	os.WriteFile(filepath.Join(projectDir, "only-session.jsonl"), []byte("{}"), 0644)

	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)
	writeStateFile(t, configBase, "tmux-x.state.json", &claudeSessionState{
		SessionID: "only-session",
	})

	unclaimed := findUnclaimedSessions(workDir)
	if len(unclaimed) != 0 {
		t.Errorf("expected 0 unclaimed sessions, got %d: %v", len(unclaimed), unclaimed)
	}
}

// TestFindUnclaimedSessions_EmptyWhenNoSessions verifies that an empty slice
// is returned when no .jsonl files exist at all.
func TestFindUnclaimedSessions_EmptyWhenNoSessions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	unclaimed := findUnclaimedSessions("/nonexistent/project")
	if len(unclaimed) != 0 {
		t.Errorf("expected 0 unclaimed sessions, got %d", len(unclaimed))
	}
}

// TestFindUnclaimedSessions_SortedByMtime verifies that unclaimed sessions are
// returned most-recent-first. This matters because restartClaudeWithResume
// should pick the latest session when scanning.
func TestFindUnclaimedSessions_SortedByMtime(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	// Also need an empty state dir so claimedSessionIDs doesn't fail
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	// Create files with different mtimes (older first)
	oldest := filepath.Join(projectDir, "oldest.jsonl")
	middle := filepath.Join(projectDir, "middle.jsonl")
	newest := filepath.Join(projectDir, "newest.jsonl")

	os.WriteFile(oldest, []byte("{}"), 0644)
	os.WriteFile(middle, []byte("{}"), 0644)
	os.WriteFile(newest, []byte("{}"), 0644)

	// Set explicit modification times to guarantee order
	now := time.Now()
	os.Chtimes(oldest, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	os.Chtimes(middle, now.Add(-1*time.Hour), now.Add(-1*time.Hour))
	os.Chtimes(newest, now, now)

	unclaimed := findUnclaimedSessions(workDir)
	if len(unclaimed) != 3 {
		t.Fatalf("expected 3 unclaimed sessions, got %d", len(unclaimed))
	}
	if unclaimed[0].Name != "newest" {
		t.Errorf("expected first session to be 'newest', got %q", unclaimed[0].Name)
	}
	if unclaimed[1].Name != "middle" {
		t.Errorf("expected second session to be 'middle', got %q", unclaimed[1].Name)
	}
	if unclaimed[2].Name != "oldest" {
		t.Errorf("expected third session to be 'oldest', got %q", unclaimed[2].Name)
	}
}

// TestResolveSessionID_PreservesValidSessionID verifies that resolveSessionID
// does NOT overwrite state.SessionID when it's already valid (the .jsonl exists).
// Without this, two claudebar sessions sharing a work directory would both resume
// the latest session, causing conversation cross-contamination.
func TestResolveSessionID_PreservesValidSessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	// Create two session files — the state already points to the older one
	now := time.Now()
	os.WriteFile(filepath.Join(projectDir, "my-session.jsonl"), []byte("{}"), 0644)
	os.Chtimes(filepath.Join(projectDir, "my-session.jsonl"), now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	os.WriteFile(filepath.Join(projectDir, "newer-session.jsonl"), []byte("{}"), 0644)
	os.Chtimes(filepath.Join(projectDir, "newer-session.jsonl"), now, now)

	// Need empty state dir so claimedSessionIDs doesn't fail
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	state := &claudeSessionState{
		SessionID:      "my-session",
		PermissionMode: "default",
		WorkDir:        workDir,
	}

	got := resolveSessionID(state)
	if got != "my-session" {
		t.Errorf("expected SessionID to be preserved as 'my-session', got %q", got)
	}
}

// TestResolveSessionID_ScansWhenEmpty verifies that resolveSessionID scans for
// the latest session when state.SessionID is empty (e.g., first restart after
// a fresh session).
func TestResolveSessionID_ScansWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	os.WriteFile(filepath.Join(projectDir, "found-session.jsonl"), []byte("{}"), 0644)

	// Need empty state dir so claimedSessionIDs doesn't fail
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	state := &claudeSessionState{
		SessionID:      "", // empty — should trigger scan
		PermissionMode: "default",
		WorkDir:        workDir,
	}

	got := resolveSessionID(state)
	if got != "found-session" {
		t.Errorf("expected SessionID to be 'found-session', got %q", got)
	}
}

// TestResolveSessionID_ScansWhenJsonlGone verifies that resolveSessionID
// re-scans when the .jsonl for the stored session ID has been deleted
// (e.g., user ran `claude --reset` or cleaned up old sessions).
func TestResolveSessionID_ScansWhenJsonlGone(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	workDir := "/Users/gk/work/project"
	encoded := "-Users-gk-work-project"
	projectDir := filepath.Join(tmp, ".claude", "projects", encoded)
	os.MkdirAll(projectDir, 0755)

	// Only the fallback session exists — the original was deleted
	os.WriteFile(filepath.Join(projectDir, "fallback-session.jsonl"), []byte("{}"), 0644)

	// Need empty state dir so claimedSessionIDs doesn't fail
	configBase := filepath.Join(tmp, "Library", "Application Support", "claudebar")
	os.MkdirAll(configBase, 0755)

	state := &claudeSessionState{
		SessionID:      "deleted-session", // .jsonl no longer exists
		PermissionMode: "default",
		WorkDir:        workDir,
	}

	got := resolveSessionID(state)
	if got != "fallback-session" {
		t.Errorf("expected SessionID to be 'fallback-session', got %q", got)
	}
}

// setLiveSessions overrides liveTmuxSessionsFunc for tests and returns a cleanup function.
func setLiveSessions(t *testing.T, names ...string) {
	t.Helper()
	old := liveTmuxSessionsFunc
	t.Cleanup(func() { liveTmuxSessionsFunc = old })
	m := make(map[string]bool)
	for _, n := range names {
		m[n] = true
	}
	liveTmuxSessionsFunc = func() map[string]bool { return m }
}

// writeStateFile is a test helper that writes a claudeSessionState to a JSON file.
func writeStateFile(t *testing.T, dir, filename string, state *claudeSessionState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshaling state file %s: %v", filename, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0644); err != nil {
		t.Fatalf("writing state file %s: %v", filename, err)
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
