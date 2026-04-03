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

		// Match by WorkDir from saved state, not by session name
		state := loadState(name)
		if state.WorkDir == dir {
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

// runDefault: attach to existing session for this cwd, or start a new one
func runDefault() {
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "claudebar requires tmux — install with: brew install tmux")
		os.Exit(1)
	}

	// Clean up orphaned CCR from prior kill-session
	cleanupOrphanedCCR()

	if isInsideClaudebar() {
		fmt.Println("Already inside claudebar. Use ⌥H for shortcuts.")
		os.Exit(1)
	}

	dir, _ := os.Getwd()
	matching, _ := listSessions()

	if len(matching) == 1 && len(claudeFlags()) == 0 {
		// Exactly one session for this dir, no extra flags — reattach
		tmuxExec("attach-session", "-t", matching[0].Name)
		return
	}

	if len(matching) > 1 && len(claudeFlags()) == 0 {
		// Multiple sessions for this dir — show picker (cwd sessions only)
		dirName := filepath.Base(dir)
		m := newPicker(matching, nil, dirName)
		p := tea.NewProgram(m)
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		result := finalModel.(pickerModel).result
		if result == nil {
			return
		}
		switch result.action {
		case "attach":
			tmuxExec("attach-session", "-t", result.session)
		case "new":
			startNew(claudeFlags())
		}
		return
	}

	// No matching sessions — check for unclaimed claude sessions to resume
	if len(claudeFlags()) == 0 {
		unclaimed := findUnclaimedSessions(dir)
		if len(unclaimed) > 0 {
			dirName := filepath.Base(dir)
			m := newPickerWithResume(unclaimed, dirName)
			p := tea.NewProgram(m)
			finalModel, err := p.Run()
			if err != nil {
				fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
				os.Exit(1)
			}
			result := finalModel.(pickerModel).result
			if result == nil {
				return
			}
			switch result.action {
			case "resume":
				startWithResume(result.session)
			case "new":
				startNew(claudeFlags())
			}
			return
		}
	}

	// No matching sessions, no unclaimed sessions, or flags specified — start fresh
	startNew(claudeFlags())
}

// runSessions: show all claudebar sessions across all projects
func runSessions() {
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "claudebar requires tmux — install with: brew install tmux")
		os.Exit(1)
	}

	if isInsideClaudebar() {
		fmt.Println("Already inside claudebar. Use ⌥H for shortcuts.")
		os.Exit(1)
	}

	matching, others := listSessions()
	total := len(matching) + len(others)

	if total == 0 {
		fmt.Println("No active claudebar sessions.")
		return
	}

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
		return
	}

	switch result.action {
	case "attach":
		tmuxExec("attach-session", "-t", result.session)
	case "resume":
		startWithResume(result.session)
	case "new":
		startNew(claudeFlags())
	}
}

func startNew(extraArgs []string) {
	startSession("", extraArgs)
}

func startWithResume(sessionID string) {
	startSession(sessionID, nil)
}

// startSession creates a new tmux session running claude. When resumeSessionID
// is non-empty, claude is launched with --resume and extraArgs/CLI flag overrides
// are not applied. When empty, it's a fresh start with extraArgs passed through.
func startSession(resumeSessionID string, extraArgs []string) {
	if _, err := exec.LookPath("claude"); err != nil {
		fmt.Fprintln(os.Stderr, "claudebar requires claude — install with: npm install -g @anthropic-ai/claude-code")
		os.Exit(1)
	}

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

	// Apply global config defaults
	cfg := loadConfig()
	permMode := "default"
	if cfg.PermissionMode != "" {
		permMode = cfg.PermissionMode
	}
	state := &claudeSessionState{
		PermissionMode: permMode,
		RemoteControl:  cfg.RemoteControl,
		WorkDir:        dir,
		Model:          cfg.Model,
	}
	if len(cfg.Features) > 0 {
		state.Features = make(map[string]bool)
		for k, v := range cfg.Features {
			state.Features[k] = v
		}
	}

	// Extract --router flag from args before passing to claude
	var routerFlag string
	if len(extraArgs) > 0 {
		routerFlag, extraArgs = extractRouterFlag(extraArgs)
	}

	// Determine active router: CLI flag > config default
	activeRouter := ""
	if routerFlag != "" {
		if _, ok := cfg.RouterConfigs[routerFlag]; !ok {
			var available []string
			for k := range cfg.RouterConfigs {
				available = append(available, k)
			}
			if len(available) == 0 {
				fmt.Fprintf(os.Stderr, "Router config %q not found (no router configs defined)\n", routerFlag)
			} else {
				fmt.Fprintf(os.Stderr, "Router config %q not found (available: %s)\n", routerFlag, strings.Join(available, ", "))
			}
			os.Exit(1)
		}
		activeRouter = routerFlag
	} else if cfg.Router != "" {
		activeRouter = cfg.Router
	}
	state.Router = activeRouter

	// Validate the router config before proceeding
	if activeRouter != "" {
		if err := validateRouterConfig(activeRouter, cfg.RouterConfigs[activeRouter]); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid router config: %v\n", err)
			os.Exit(1)
		}
	}

	resume := false
	if resumeSessionID != "" {
		state.SessionID = resumeSessionID
		resume = true
	} else {
		// CLI flags override config defaults only for fresh starts
		for _, arg := range extraArgs {
			if arg == "--dangerously-skip-permissions" {
				state.PermissionMode = "bypassPermissions"
			}
		}
	}

	// If router is active, ensure CCR is running before creating the session
	if state.Router != "" {
		if err := ensureCCRRunning(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Router error: %v\n", err)
			os.Exit(1)
		}
	}

	saveState(name, state)
	claudeCmd := launchClaudeWithExtra(name, state, resume, extraArgs)

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
	for _, env := range routerEnvVars(state.Router, lookupRouterConfig(state.Router)) {
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
	sess := currentSession()

	state := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}

	state.SessionID = resolveSessionID(state)
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
	sess := currentSession()
	state := loadState(sess)
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

func runToggleRC() {
	sess := currentSession()
	state := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}

	state.RemoteControl = !state.RemoteControl
	action := "ON"
	if !state.RemoteControl {
		action = "OFF"
	}
	tmuxExec("display-message", fmt.Sprintf("Remote Control → %s (restarting...)", action))
	restartClaudeWithResume(sess, state)
}

// togglePane checks if a tracked pane exists and kills it (toggle off),
// or creates a new one with the given command (toggle on).
func togglePane(optionName, cmd, direction, size string) {
	paneID, _ := tmuxOutput("show-options", "-v", optionName)
	if paneID != "" {
		existing, _ := tmuxOutput("list-panes", "-F", "#{pane_id}")
		if strings.Contains(existing, paneID) {
			tmuxExec("kill-pane", "-t", paneID)
			tmuxExec("set-option", "-u", optionName)
			return
		}
		tmuxExec("set-option", "-u", optionName)
	}

	newPaneID, err := tmuxOutput("split-window", direction, "-l", size, "-P", "-F", "#{pane_id}", cmd)
	if err == nil && newPaneID != "" {
		tmuxExec("set-option", optionName, newPaneID)
	}
}

func runTasks() {
	sess := currentSession()
	taskListID := taskListIDForSession(sess)
	self := selfPath()
	taskCmd := fmt.Sprintf("CLAUDEBAR_TASK_LIST_ID=%s %s _taskview",
		shellQuote(taskListID), shellQuote(self))
	togglePane("@claudebar-tasks-pane", taskCmd, "-h", "35%")
}

func runAgents() {
	sess := currentSession()
	state := loadState(sess)

	// Check if agent teams are enabled
	if !state.isFeatureOn("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS") {
		tmuxExec("display-message", "Agent Teams not enabled — toggle in Features menu (⚙)")
		return
	}

	self := selfPath()
	workDir := state.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	agentCmd := fmt.Sprintf("CLAUDEBAR_CWD=%s %s _agentview",
		shellQuote(workDir), shellQuote(self))
	togglePane("@claudebar-agents-pane", agentCmd, "-h", "35%")
}

// featureState returns ○ OFF, ● ON, or ◉ ALWAYS for a feature
// based on session state and global config.
func featureState(sessionOn, configOn bool) string {
	if configOn {
		return "◉ ALWAYS"
	}
	if sessionOn {
		return "●    ON"
	}
	return "○   OFF"
}

// cycleFeature advances: OFF → ON → ALWAYS → OFF
// Returns new session value, new config value
func cycleFeature(sessionOn, configOn bool) (bool, bool) {
	if configOn {
		// ALWAYS → OFF
		return false, false
	}
	if sessionOn {
		// ON → ALWAYS
		return true, true
	}
	// OFF → ON
	return true, false
}

func runFeatures() {
	self := selfPath()
	sess := currentSession()
	state := loadState(sess)
	cfg := loadConfig()

	var menuArgs []string
	menuArgs = append(menuArgs, "-T", " #[fg=#00d4ff,bold]Features  ○ off  ● on  ◉ always ", "-x", "R", "-y", "S")

	// Bypass permissions
	menuArgs = append(menuArgs,
		fmt.Sprintf("%s  Bypass permissions", featureState(state.PermissionMode == "bypassPermissions", cfg.PermissionMode == "bypassPermissions")),
		"p",
		fmt.Sprintf("run-shell '%s _toggle bypass_permissions'", self),
	)

	// Remote control
	menuArgs = append(menuArgs,
		fmt.Sprintf("%s  Remote Control", featureState(state.RemoteControl, cfg.RemoteControl)),
		"r",
		fmt.Sprintf("run-shell '%s _toggle remote_control'", self),
	)

	menuArgs = append(menuArgs, "")

	// Env var features
	for _, envVar := range featureOrder {
		f := featureRegistry[envVar]
		menuArgs = append(menuArgs,
			fmt.Sprintf("%s  %s", featureState(state.isFeatureOn(envVar), cfg.Features[envVar]), f.Label),
			strings.ToLower(string(f.Label[0])),
			fmt.Sprintf("run-shell '%s _toggle %s'", self, envVar),
		)
	}

	tmuxExec(append([]string{"display-menu"}, menuArgs...)...)
}

func runToggleFeature(envVar string) {
	sess := currentSession()
	state := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}
	cfg := loadConfig()

	switch envVar {
	case "bypass_permissions":
		sessionOn := state.PermissionMode == "bypassPermissions"
		configOn := cfg.PermissionMode == "bypassPermissions"
		newSession, newConfig := cycleFeature(sessionOn, configOn)
		if newSession {
			state.PermissionMode = "bypassPermissions"
		} else {
			state.PermissionMode = "default"
		}
		if newConfig {
			cfg.PermissionMode = "bypassPermissions"
		} else {
			cfg.PermissionMode = ""
		}

	case "remote_control":
		newSession, newConfig := cycleFeature(state.RemoteControl, cfg.RemoteControl)
		state.RemoteControl = newSession
		cfg.RemoteControl = newConfig

	default:
		// Env var feature
		sessionOn := state.isFeatureOn(envVar)
		configOn := cfg.Features[envVar]
		newSession, newConfig := cycleFeature(sessionOn, configOn)

		if state.Features == nil {
			state.Features = make(map[string]bool)
		}
		state.Features[envVar] = newSession

		if cfg.Features == nil {
			cfg.Features = make(map[string]bool)
		}
		cfg.Features[envVar] = newConfig
	}

	saveConfig(cfg)

	label := envVar
	if f, ok := featureRegistry[envVar]; ok {
		label = f.Label
	}
	switch envVar {
	case "bypass_permissions":
		label = "Bypass permissions"
	case "remote_control":
		label = "Remote Control"
	}

	// Determine new display state for message
	var newState string
	switch envVar {
	case "bypass_permissions":
		newState = featureState(state.PermissionMode == "bypassPermissions", cfg.PermissionMode == "bypassPermissions")
	case "remote_control":
		newState = featureState(state.RemoteControl, cfg.RemoteControl)
	default:
		newState = featureState(state.isFeatureOn(envVar), cfg.Features[envVar])
	}

	// Only restart if the session state actually changed
	var sessionChanged bool
	switch envVar {
	case "bypass_permissions":
		old := loadState(sess)
		sessionChanged = (old.PermissionMode != state.PermissionMode)
	case "remote_control":
		old := loadState(sess)
		sessionChanged = (old.RemoteControl != state.RemoteControl)
	default:
		old := loadState(sess)
		sessionChanged = (old.isFeatureOn(envVar) != state.isFeatureOn(envVar))
	}

	if sessionChanged {
		tmuxExec("display-message", fmt.Sprintf("%s → %s (restarting...)", label, newState))
		restartClaudeWithResume(sess, state)
	} else {
		tmuxExec("display-message", fmt.Sprintf("%s → %s", label, newState))
		saveState(sess, state)
	}
}

func runShell() {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	togglePane("@claudebar-shell-pane", fmt.Sprintf("%s -l", shell), "-v", "30%")
}

// runMenu: show the full action menu as a tmux display-menu (for ⌥M keyboard shortcut)
func runMenu() {
	self := selfPath()
	args := []string{"display-menu", "-T", " #[fg=#00d4ff,bold]claudebar ", "-x", "R", "-y", "S"}
	for _, m := range menuItems {
		if m.Label == "" {
			args = append(args, "")
			continue
		}
		action := fmt.Sprintf("run-shell '%s %s'", self, m.Action)
		if m.Confirm != "" {
			action = fmt.Sprintf("confirm-before -p '%s' \"%s\"", m.Confirm, action)
		}
		args = append(args, m.Label, m.Key, action)
	}
	tmuxExec(args...)
}

// runSendToPane types a command into the claude pane and presses enter
func runSendToPane(cmd string) {
	tmuxExec("send-keys", "-t", ":.0", "-l", cmd)
	tmuxExec("send-keys", "-t", ":.0", "Enter")
}

func runKillSession() {
	sess := currentSession()
	tmuxExec("kill-session", "-t", sess)
}

func runHelpPopup() {
	self := selfPath()
	tmuxExec("display-popup", "-w", "55", "-h", "24", "-T", " claudebar ",
		fmt.Sprintf("%s help", self))
}

func runHelp() {
	fmt.Print(`claudebar — tmux harness for Claude Code

USAGE
  claudebar                    Attach to session for cwd, or start new
  claudebar [claude flags]     Start new session with flags passed to claude
  claudebar sessions (s)       List and manage all sessions across projects

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
  🌙 OFF-PEAK  Outside peak hours
`)
}
