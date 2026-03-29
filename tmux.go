package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	sessionPrefix = "claudebar"
	tmuxConfName  = "claudebar.tmux.conf"
	tmuxSocket    = "claudebar"
)

// menuItem represents one entry in the claudebar menu.
// A zero-value item (Label == "") renders as a separator.
type menuItem struct {
	Label   string // display text
	Key     string // tmux shortcut key
	Action  string // claudebar subcommand, e.g. "_detach"
	Confirm string // if set, wrap action with tmux confirm-before using this prompt
}

// menuItems is the single source of truth for the claudebar menu.
var menuItems = []menuItem{
	{"⏏ Background                 ⌥W", "b", "_detach", ""},
	{"✕ Kill session                  ", "x", "_kill", "Kill this session? (y/n)"},
	{},
	{"⟳ Toggle bypass permissions     ", "p", "_perms", ""},
	{"~ Toggle remote control         ", "r", "_rc", ""},
	{},
	{"⚙ Features & defaults           ", "f", "_features", ""},
	{},
	{"$ Shell pane                 ⌥S", "s", "_shell", ""},
	{"# Tasks pane                 ⌥T", "t", "_tasks", ""},
	{"@ Agent teams pane           ⌥A", "a", "_agents", ""},
	{},
	{"» Compact context               ", "k", "_compact", ""},
	{"↺ Clear / new chat              ", "n", "_clear", ""},
	{"§ Toggle verbose                ", "v", "_verbose", ""},
	{"∿ Show usage                    ", "c", "_usage", ""},
	{},
	{"? Help                       ⌥H", "h", "_help", ""},
	{"↑ Update Claude Code            ", "u", "_upgrade", ""},
}

func tmuxExec(args ...string) error {
	fullArgs := append([]string{"-L", tmuxSocket}, args...)
	cmd := exec.Command("tmux", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func tmuxOutput(args ...string) (string, error) {
	fullArgs := append([]string{"-L", tmuxSocket}, args...)
	cmd := exec.Command("tmux", fullArgs...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func editorCmd(file string) *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	return exec.Command(editor, file)
}

func currentSession() string {
	sess, _ := tmuxOutput("display-message", "-p", "#{session_name}")
	return strings.TrimSpace(sess)
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "claudebar"
	}
	return p
}

func sessionName() string {
	dir, err := os.Getwd()
	if err != nil {
		return sessionPrefix
	}
	name := filepath.Base(dir)
	// Sanitize characters that tmux interprets specially in session names:
	// dots (session.window parsing) and colons (session:window parsing)
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

func isInsideClaudebar() bool {
	return os.Getenv("CLAUDEBAR") == "1"
}

func mainPaneFile(sessionName string) string {
	return filepath.Join(stateDir(), sessionName+".main-pane")
}

func saveMainPaneID(sessionName, paneID string) {
	os.WriteFile(mainPaneFile(sessionName), []byte(paneID), 0644)
}

func mainPaneAlive() bool {
	// Read session name from tmux
	sess := currentSession()
	if sess == "" {
		return false
	}
	data, err := os.ReadFile(mainPaneFile(sess))
	if err != nil || len(data) == 0 {
		return false
	}
	mainID := strings.TrimSpace(string(data))

	panes, err := tmuxOutput("list-panes", "-F", "#{pane_id}")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(panes, "\n") {
		if strings.TrimSpace(line) == mainID {
			return true
		}
	}
	return false
}

func sessionExists(name string) bool {
	fullArgs := []string{"-L", tmuxSocket, "has-session", "-t", name}
	err := exec.Command("tmux", fullArgs...).Run()
	return err == nil
}

func menuCmd(self string) string {
	// Inline display-menu so click-and-drag highlighting works
	var b strings.Builder
	b.WriteString(`display-menu -T " #[fg=#00d4ff,bold]claudebar " -x R -y S `)
	for _, m := range menuItems {
		if m.Label == "" {
			b.WriteString(`"" `)
			continue
		}
		runShell := `run-shell '` + self + ` ` + m.Action + `'`
		if m.Confirm != "" {
			// confirm-before needs the run-shell command in double quotes inside,
			// and the whole thing must NOT be re-wrapped in double quotes
			b.WriteString(`"` + m.Label + `" ` + m.Key + ` "confirm-before -p '` + m.Confirm + `' \"` + runShell + `\"" `)
		} else {
			b.WriteString(`"` + m.Label + `" ` + m.Key + ` "` + runShell + `" `)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func generateTmuxConf() string {
	s := selfPath()
	menu := menuCmd(s)

	conf := `# claudebar tmux config (auto-generated)

# Prefix set to C-] (unused by claude, keeps tmux happy)
set -g prefix C-]
unbind C-b

# Mouse support
set -g mouse on

# Two-line status bar
set -g status 2
set -g status-position bottom
set -g status-interval 5
set -g status-style "bg=#1a1a2e,fg=#e0e0e0"

# Line 1: session info + peak indicator
set -g status-format[0] "#[bg=#16213e,fg=#00d4ff,bold] claudebar #[bg=#1a1a2e,fg=#888888] #{session_name} #[default]#[align=right]#(SELF _status)"

# Line 2: shortcut hints
set -g status-format[1] " #[fg=#00d4ff]⌥W #[fg=#888888]background  #[fg=#ffd700]⌥S #[fg=#888888]shell  #[fg=#ff6b9d]⌥T #[fg=#888888]tasks  #[fg=#00ff88]⌥A #[fg=#888888]agents  #[fg=#b388ff]⌥H #[fg=#888888]help  #[fg=#888888]⌥M #[fg=#555555]menu"

# Hide default status-left/right (using status-format instead)
set -g status-left ""
set -g status-right ""
set -g window-status-format ""
set -g window-status-current-format ""

# Pane styling
set -g pane-border-style "fg=#333333"
set -g pane-active-border-style "fg=#00d4ff"

# Direct keybindings (Alt/Option + key)
bind -n M-w run-shell "SELF _detach"
bind -n M-s run-shell "SELF _shell"
bind -n M-t run-shell "SELF _tasks"
bind -n M-a run-shell "SELF _agents"
bind -n M-h run-shell "SELF _help"
bind -n M-m run-shell "SELF _menu"

# Click anywhere on status bar → inline menu (not run-shell, so drag works)
bind -n MouseDown1Status MENU_CMD
bind -n MouseDown1StatusRight MENU_CMD
bind -n MouseDown1StatusLeft MENU_CMD
bind -n MouseDown1StatusDefault MENU_CMD

set -g remain-on-exit off
set -g exit-empty on

# Terminal settings
set -g default-terminal "xterm-256color"
set -ga terminal-overrides ",xterm-256color:Tc"
set -g history-limit 50000
set -g allow-rename off
set -sg escape-time 10
`
	conf = strings.ReplaceAll(conf, "SELF", s)
	conf = strings.ReplaceAll(conf, "MENU_CMD", menu)
	return conf
}

func confPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	claudebarDir := filepath.Join(dir, "claudebar")
	os.MkdirAll(claudebarDir, 0755)
	return filepath.Join(claudebarDir, tmuxConfName)
}

func writeConf() (string, error) {
	path := confPath()
	return path, os.WriteFile(path, []byte(generateTmuxConf()), 0644)
}
