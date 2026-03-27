package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type sessionInfo struct {
	Name string
	Ago  string
}

func listSessions() (matching []sessionInfo, others []sessionInfo) {
	out, err := tmuxOutput("list-sessions", "-F", "#{session_name}\t#{session_activity}")
	if err != nil {
		return nil, nil
	}

	dir, _ := os.Getwd()
	dirBase := filepath.Base(dir)
	expected := dirBase

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := parts[0]
		var ago string
		if len(parts) > 1 {
			if ts, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				ago = timeAgo(time.Unix(ts, 0))
			}
		}
		info := sessionInfo{Name: name, Ago: ago}
		if name == expected {
			matching = append(matching, info)
		} else {
			others = append(others, info)
		}
	}
	return matching, others
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// claudeFlags returns any extra args meant for claude (filters out claudebar's own flags)
func claudeFlags() []string {
	if len(os.Args) <= 1 {
		return nil
	}
	// Everything after the binary name is a claude flag
	// (claudebar's own commands are handled before reaching runDefault)
	return os.Args[1:]
}

// runDefault: smart launcher with TUI picker
func runDefault() {
	if isInsideClaudebar() {
		fmt.Println("Already inside claudebar. Use ⌥H for shortcuts.")
		os.Exit(1)
	}

	matching, others := listSessions()
	total := len(matching) + len(others)

	if total == 0 {
		// No sessions, start fresh
		startNew(claudeFlags())
		return
	}

	if len(matching) == 1 && len(others) == 0 && len(claudeFlags()) == 0 {
		// Exactly one session for this dir, no flags, just reattach
		tmuxExec("attach-session", "-t", matching[0].Name)
		return
	}

	// Multiple sessions or flags specified — show TUI picker
	dir, _ := os.Getwd()
	dirName := filepath.Base(dir)

	m := newPicker(matching, others, dirName)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	result := finalModel.(pickerModel).result
	if result == nil {
		// User quit without selecting
		return
	}

	switch result.action {
	case "attach":
		tmuxExec("attach-session", "-t", result.session)
	case "new":
		startNew(claudeFlags())
	}
}

func startNew(extraArgs []string) {
	confPath, err := writeConf()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write tmux config: %v\n", err)
		os.Exit(1)
	}

	dir, _ := os.Getwd()
	name := sessionName()

	// If name already taken, append a number
	if sessionExists(name) {
		for i := 2; i < 100; i++ {
			candidate := fmt.Sprintf("%s-%d", name, i)
			if !sessionExists(candidate) {
				name = candidate
				break
			}
		}
	}

	state := &claudeSessionState{
		PermissionMode: "default",
		WorkDir:        dir,
	}

	for _, arg := range extraArgs {
		if arg == "--dangerously-skip-permissions" {
			state.PermissionMode = "bypassPermissions"
		}
	}

	saveState(name, state)
	claudeCmd := launchClaudeWithExtra(state, false, extraArgs)

	taskListID := taskListIDForSession(name)

	// Source config on existing server (handles stale config from prior builds).
	// Fails silently if no server yet — -f handles fresh starts.
	exec.Command("tmux", "-L", tmuxSocket, "source-file", confPath).Run()

	// Create session detached so we can capture the main pane ID before attaching
	tmuxArgs := []string{
		"-f", confPath,
		"new-session",
		"-d",
		"-s", name,
		"-c", dir,
		"-e", "CLAUDEBAR=1",
		"-e", "CLAUDE_CODE_TASK_LIST_ID=" + taskListID,
	}
	for _, env := range state.featureEnvVars() {
		tmuxArgs = append(tmuxArgs, "-e", env)
	}
	tmuxArgs = append(tmuxArgs, claudeCmd)

	if err := tmuxExec(tmuxArgs...); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create session: %v\n", err)
		os.Exit(1)
	}

	// Store main pane ID for side panes to track
	paneID, _ := tmuxOutput("display-message", "-t", name, "-p", "#{pane_id}")
	if paneID != "" {
		saveMainPaneID(name, paneID)
	}

	// Attach
	tmuxExec("attach-session", "-t", name)
}

// --- Internal commands (called by tmux keybinds) ---

func runDetach() {
	tmuxExec("detach-client")
}

func runUpgrade() {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")

	state, _ := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}

	// Find latest session ID before we restart
	sessionID := findLatestClaudeSession(state.WorkDir)
	if sessionID != "" {
		state.SessionID = sessionID
	}
	saveState(sess, state)

	// Run npm upgrade, then restart claude with resume
	// npm runs in current process (blocking), then we respawn the pane
	npmPath, _ := exec.LookPath("npm")
	if npmPath == "" {
		tmuxExec("display-message", "npm not found — cannot upgrade")
		return
	}
	cmd := exec.Command(npmPath, "update", "-g", "@anthropic-ai/claude-code")
	output, err := cmd.CombinedOutput()
	if err != nil {
		tmuxExec("display-message", fmt.Sprintf("Upgrade failed: %s", truncate(string(output), 60)))
		return
	}

	tmuxExec("display-message", "Upgraded! Restarting...")
	restartClaudeWithResume(sess, state)
}

func runPerms() {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	state, _ := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}

	state.PermissionMode = toggleBypassPermissions(state.PermissionMode)

	action := "ON"
	if state.PermissionMode == "default" {
		action = "OFF"
	}
	msg := fmt.Sprintf("Bypass permissions → %s (restarting...)", action)
	tmuxExec("display-message", msg)
	restartClaudeWithResume(sess, state)
}

func runTasks() {
	// Check if we have a tracked tasks pane that's still alive
	paneID, _ := tmuxOutput("show-options", "-v", "@claudebar-tasks-pane")
	if paneID != "" {
		existing, _ := tmuxOutput("list-panes", "-F", "#{pane_id}")
		if strings.Contains(existing, paneID) {
			tmuxExec("kill-pane", "-t", paneID)
			tmuxExec("set-option", "-u", "@claudebar-tasks-pane")
			return
		}
		tmuxExec("set-option", "-u", "@claudebar-tasks-pane")
	}

	// Get the session name to derive the task list ID
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	taskListID := taskListIDForSession(sess)
	self := selfPath()

	// Launch our built-in task viewer in a side pane
	taskCmd := fmt.Sprintf("CLAUDEBAR_TASK_LIST_ID=%s %s _taskview",
		shellQuote(taskListID), shellQuote(self))

	newPaneID, err := tmuxOutput("split-window", "-h", "-l", "35%", "-P", "-F", "#{pane_id}", taskCmd)
	if err == nil && newPaneID != "" {
		tmuxExec("set-option", "@claudebar-tasks-pane", newPaneID)

		tmuxExec("select-pane", "-t", ":.0")
	}
}

func runAgents() {
	// Check if we have a tracked agents pane that's still alive
	paneID, _ := tmuxOutput("show-options", "-v", "@claudebar-agents-pane")
	if paneID != "" {
		existing, _ := tmuxOutput("list-panes", "-F", "#{pane_id}")
		if strings.Contains(existing, paneID) {
			tmuxExec("kill-pane", "-t", paneID)
			tmuxExec("set-option", "-u", "@claudebar-agents-pane")
			return
		}
		tmuxExec("set-option", "-u", "@claudebar-agents-pane")
	}

	self := selfPath()
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	state, _ := loadState(sess)
	workDir := state.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	agentCmd := fmt.Sprintf("CLAUDEBAR_CWD=%s %s _agentview",
		shellQuote(workDir), shellQuote(self))

	newPaneID, err := tmuxOutput("split-window", "-h", "-l", "35%", "-P", "-F", "#{pane_id}", agentCmd)
	if err == nil && newPaneID != "" {
		tmuxExec("set-option", "@claudebar-agents-pane", newPaneID)

		tmuxExec("select-pane", "-t", ":.0")
	}
}

func runFeatures() {
	self := selfPath()
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	state, _ := loadState(sess)

	var menuArgs []string
	menuArgs = append(menuArgs, "-T", " #[fg=#00d4ff,bold]Features (toggle & restart) ", "-x", "R", "-y", "S")

	for _, envVar := range featureOrder {
		f := featureRegistry[envVar]
		indicator := "○"
		if state.isFeatureOn(envVar) {
			indicator = "●"
		}
		// Use first unique char as shortcut key
		key := strings.ToLower(string(f.Label[0]))
		menuArgs = append(menuArgs,
			fmt.Sprintf("%s  %s", indicator, f.Label),
			key,
			fmt.Sprintf("run-shell '%s _toggle %s'", self, envVar),
		)
	}

	tmuxExec(append([]string{"display-menu"}, menuArgs...)...)
}

func runToggleFeature(envVar string) {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	state, _ := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}

	f, ok := featureRegistry[envVar]
	label := envVar
	if ok {
		label = f.Label
	}

	state.toggleFeature(envVar)
	action := "ON"
	if !state.isFeatureOn(envVar) {
		action = "OFF"
	}

	tmuxExec("display-message", fmt.Sprintf("%s → %s (restarting...)", label, action))
	restartClaudeWithResume(sess, state)
}

func runShell() {
	// Check if we have a tracked shell pane that's still alive
	paneID, _ := tmuxOutput("show-options", "-v", "@claudebar-shell-pane")
	if paneID != "" {
		existing, _ := tmuxOutput("list-panes", "-F", "#{pane_id}")
		if strings.Contains(existing, paneID) {
			tmuxExec("kill-pane", "-t", paneID)
			tmuxExec("set-option", "-u", "@claudebar-shell-pane")
			return
		}
		tmuxExec("set-option", "-u", "@claudebar-shell-pane")
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	newPaneID, err := tmuxOutput("split-window", "-v", "-l", "30%", "-P", "-F", "#{pane_id}",
		fmt.Sprintf("%s -l", shell))
	if err == nil && newPaneID != "" {
		tmuxExec("set-option", "@claudebar-shell-pane", newPaneID)

		tmuxExec("select-pane", "-t", ":.0")
	}
}

// runMenu: show the full action menu as a tmux display-menu (for ⌥M keyboard shortcut)
func runMenu() {
	self := selfPath()
	tmuxExec("display-menu", "-T", " #[fg=#00d4ff,bold]claudebar ", "-x", "R", "-y", "S",
		// Session
		"⏏ Background                 ⌥W", "b", fmt.Sprintf("run-shell '%s _detach'", self),
		"✕ Kill session                  ", "x", fmt.Sprintf("run-shell '%s _kill'", self),
		"",
		// Config
		"⟳ Toggle bypass permissions     ", "p", fmt.Sprintf("run-shell '%s _perms'", self),
		"↑ Update Claude Code            ", "u", fmt.Sprintf("run-shell '%s _upgrade'", self),
		"⚙ Features (env toggles)        ", "f", fmt.Sprintf("run-shell '%s _features'", self),
		"",
		// Panes
		"$ Toggle shell pane          ⌥S", "s", fmt.Sprintf("run-shell '%s _shell'", self),
		"☰ Toggle tasks pane          ⌥T", "t", fmt.Sprintf("run-shell '%s _tasks'", self),
		"🤖 Toggle agents pane         ⌥A", "a", fmt.Sprintf("run-shell '%s _agents'", self),
		"",
		// Claude commands
		"🗜 Compact context               ", "k", fmt.Sprintf("run-shell '%s _compact'", self),
		"🔄 Clear / new chat              ", "n", fmt.Sprintf("run-shell '%s _clear'", self),
		"📝 Toggle verbose                ", "v", fmt.Sprintf("run-shell '%s _verbose'", self),
		"📊 Show usage                    ", "c", fmt.Sprintf("run-shell '%s _usage'", self),
		"",
		"? Help                       ⌥H", "h", fmt.Sprintf("run-shell '%s _help'", self),
	)
}

// runSendToPane: type a command into the claude pane and press enter
func runSendToPane(cmd string) {
	// Send to pane 0 (the claude pane)
	tmuxExec("send-keys", "-t", ":.0", cmd, "Enter")
}

// runKillSession: actually kill the tmux session (quit claude)
func runKillSession() {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	tmuxExec("kill-session", "-t", sess)
}

// runCleanup kills the current tmux session after claude exits naturally.
// Only reached on natural exit — respawn-pane -k sends SIGKILL which kills
// the whole process chain before this runs.
func runCleanup() {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	if sess != "" {
		tmuxExec("kill-session", "-t", sess)
	}
}

// runCheckMain checks if the main claude pane still exists. If not, kills the session.
// Called by the pane-exited hook — instant, no polling.
func runCheckMain() {
	mainID, err := tmuxOutput("show-option", "-v", "@claudebar-main")
	if err != nil || mainID == "" {
		return
	}
	panes, err := tmuxOutput("list-panes", "-F", "#{pane_id}")
	if err != nil {
		return
	}
	for _, line := range strings.Split(panes, "\n") {
		if strings.TrimSpace(line) == mainID {
			return // main pane still alive
		}
	}
	// Main pane is gone — kill session
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	if sess != "" {
		tmuxExec("kill-session", "-t", sess)
	}
}

func runHelpPopup() {
	self := selfPath()
	tmuxExec("display-popup", "-w", "55", "-h", "16", "-T", " claudebar ",
		fmt.Sprintf("%s help", self))
}

func runHelp() {
	fmt.Print(`claudebar — tmux harness for Claude Code

USAGE
  claudebar                    Launch or resume (interactive picker)
  claudebar [claude flags]     Picker, with flags applied to new sessions

  All flags are passed through to claude:
    claudebar --dangerously-skip-permissions
    claudebar --model sonnet --verbose

SHORTCUTS (inside claudebar)
  ⌥W     Background — claude keeps running
  ⌥S     Toggle shell pane
  ⌥T     Toggle tasks pane
  ⌥A     Toggle agents pane
  ⌥H     Show this help
  ⌥M     Open action menu

  Click the status bar for the menu too.

STATUS BAR
  ⚡PEAK      Peak hours (weekdays 5-11am PT, shown in local time)
  ○ off-peak  Outside peak hours
`)
}
