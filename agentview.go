package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// --- Data types ---

type teamConfig struct {
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	CreatedAt     int64        `json:"createdAt"`
	LeadSessionID string       `json:"leadSessionId"`
	LeadAgentID   string       `json:"leadAgentId"`
	Members       []teamMember `json:"members"`
}

type teamMember struct {
	Name      string `json:"name"`
	AgentID   string `json:"agentId"`
	AgentType string `json:"agentType"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Color     string `json:"color"`
	CWD       string `json:"cwd"`
}

type inboxMessage struct {
	From      string `json:"from"`
	Text      string `json:"text"`
	Summary   string `json:"summary"`
	Timestamp any    `json:"timestamp"` // can be string or int64
	Read      bool   `json:"read"`
}

func (m inboxMessage) time() time.Time {
	switch v := m.Timestamp.(type) {
	case string:
		t, _ := time.Parse(time.RFC3339Nano, v)
		return t
	case float64:
		return time.Unix(int64(v)/1000, 0)
	}
	return time.Time{}
}

func (m inboxMessage) displayText() string {
	// Try to parse JSON task assignments and display nicely
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(m.Text), &parsed); err == nil {
		if t, ok := parsed["type"].(string); ok {
			switch t {
			case "task_assignment":
				subject, _ := parsed["subject"].(string)
				return fmt.Sprintf("[task] %s", subject)
			default:
				return fmt.Sprintf("[%s]", t)
			}
		}
	}
	if m.Summary != "" {
		return m.Summary
	}
	return m.Text
}

type subagentInfo struct {
	ID       string
	ModTime  time.Time
	LastLine string
}

// --- Data loading ---

// listTeamsForProject returns teams whose cwd matches the given project directory
func listTeamsForProject(projectDir string) []string {
	home, _ := os.UserHomeDir()
	teamsDir := filepath.Join(home, ".claude", "teams")
	entries, err := os.ReadDir(teamsDir)
	if err != nil {
		return nil
	}
	var teams []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		teamDir := filepath.Join(teamsDir, e.Name())

		// Check config.json for cwd match
		cfg, err := loadTeamConfig(e.Name())
		if err == nil {
			// Check if any member's cwd is under the project dir
			for _, m := range cfg.Members {
				if m.CWD != "" && strings.HasPrefix(m.CWD, projectDir) {
					teams = append(teams, e.Name())
					break
				}
			}
			continue
		}

		// No config — check if inboxes exist (stale team)
		if _, err := os.Stat(filepath.Join(teamDir, "inboxes")); err == nil {
			// Can't verify project scope without config, skip
			_ = teamDir
		}
	}
	return teams
}

func loadTeamConfig(teamName string) (*teamConfig, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude", "teams", teamName, "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg teamConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadInbox(teamName, agentName string) []inboxMessage {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude", "teams", teamName, "inboxes", agentName+".json"))
	if err != nil {
		return nil
	}
	var msgs []inboxMessage
	json.Unmarshal(data, &msgs)
	return msgs
}

func findSubagentTranscripts() []subagentInfo {
	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".claude", "projects")

	cwd := os.Getenv("CLAUDEBAR_CWD")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd == "" {
		return nil
	}

	encoded := strings.ReplaceAll(cwd, "/", "-")
	searchDir := filepath.Join(projectsDir, encoded)
	if _, err := os.Stat(searchDir); err != nil {
		return nil
	}

	seen := make(map[string]subagentInfo)

	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.Contains(path, "subagents") || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if time.Since(info.ModTime()) > 5*time.Minute {
			return nil
		}

		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		id = strings.TrimPrefix(id, "agent-")

		if existing, ok := seen[id]; ok && existing.ModTime.After(info.ModTime()) {
			return nil
		}

		seen[id] = subagentInfo{
			ID:       id,
			ModTime:  info.ModTime(),
			LastLine: readLastLine(path),
		}
		return nil
	})

	agents := make([]subagentInfo, 0, len(seen))
	for _, sa := range seen {
		agents = append(agents, sa)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ModTime.After(agents[j].ModTime)
	})

	if len(agents) > 15 {
		agents = agents[:15]
	}
	return agents
}

func readLastLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Read last 4KB instead of entire file
	const tailSize = 4096
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	f.Seek(offset, 0)
	buf := make([]byte, tailSize)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(buf[:n])), "\n")
	last := lines[len(lines)-1]

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		return truncate(last, 50)
	}

	if role, ok := entry["role"].(string); ok {
		if content, ok := entry["content"].(string); ok {
			return fmt.Sprintf("[%s] %s", role, truncate(content, 35))
		}
		if _, ok := entry["content"].([]interface{}); ok {
			return fmt.Sprintf("[%s] (tool use)", role)
		}
	}
	if t, ok := entry["type"].(string); ok {
		return fmt.Sprintf("[%s]", t)
	}
	return "(active)"
}

// --- Bubbletea TUI ---

type agentViewMode int

const (
	agentOverview agentViewMode = iota
	agentMemberDetail
	agentInboxDetail
)

type agentItem struct {
	kind     string // "team-header", "member", "subagent-header", "subagent"
	team     string
	member   *teamMember
	subagent *subagentInfo
}

type agentModel struct {
	mode           agentViewMode
	items          []agentItem
	cursor         int
	projectDir     string
	teams          []string
	subagents      []subagentInfo
	selectedTeam   string
	selectedMember *teamMember
	inbox          []inboxMessage
	width          int
	height         int
}

type agentTickMsg time.Time

func agentTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return agentTickMsg(t)
	})
}

func newAgentModel(projectDir string) agentModel {
	m := agentModel{projectDir: projectDir}
	m.refresh()
	return m
}

func (m *agentModel) refresh() {
	m.teams = listTeamsForProject(m.projectDir)
	m.subagents = findSubagentTranscripts()
	m.buildItems()
}

func (m *agentModel) buildItems() {
	var items []agentItem

	for _, teamName := range m.teams {
		items = append(items, agentItem{kind: "team-header", team: teamName})

		cfg, err := loadTeamConfig(teamName)
		if err != nil {
			continue
		}
		for i := range cfg.Members {
			items = append(items, agentItem{
				kind:   "member",
				team:   teamName,
				member: &cfg.Members[i],
			})
		}
	}

	if len(m.subagents) > 0 {
		items = append(items, agentItem{kind: "subagent-header"})
		for i := range m.subagents {
			items = append(items, agentItem{
				kind:     "subagent",
				subagent: &m.subagents[i],
			})
		}
	}

	m.items = items
	if m.cursor >= len(m.items) {
		m.cursor = 0
	}
	m.skipHeaders(1)
}

func (m *agentModel) skipHeaders(dir int) {
	for i := 0; i < len(m.items); i++ {
		if m.cursor >= 0 && m.cursor < len(m.items) {
			kind := m.items[m.cursor].kind
			if kind != "team-header" && kind != "subagent-header" {
				return
			}
		}
		m.cursor += dir
		if m.cursor >= len(m.items) {
			m.cursor = 0
		}
		if m.cursor < 0 {
			m.cursor = len(m.items) - 1
		}
	}
}

func (m agentModel) Init() tea.Cmd {
	return agentTick()
}

func (m agentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case agentTickMsg:
		if !mainPaneAlive() {
			return m, tea.Quit
		}
		m.refresh()
		return m, agentTick()

	case agentEditorDoneMsg:
		// Reload after editing
		if m.selectedMember != nil {
			m.inbox = loadInbox(m.selectedTeam, m.selectedMember.Name)
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case agentOverview:
			return m.updateOverview(msg)
		case agentMemberDetail:
			return m.updateMemberDetail(msg)
		case agentInboxDetail:
			return m.updateInboxDetail(msg)
		}
	}
	return m, nil
}

type agentEditorDoneMsg struct{ err error }

func (m agentModel) updateOverview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.cursor--
		if m.cursor < 0 {
			m.cursor = len(m.items) - 1
		}
		m.skipHeaders(-1)
	case "down", "j":
		m.cursor++
		if m.cursor >= len(m.items) {
			m.cursor = 0
		}
		m.skipHeaders(1)
	case "enter":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "member" && item.member != nil {
				m.selectedTeam = item.team
				m.selectedMember = item.member
				m.mode = agentMemberDetail
			}
		}
	}
	return m, nil
}

func (m agentModel) updateMemberDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = agentOverview
	case "i":
		// View inbox
		if m.selectedMember != nil {
			m.inbox = loadInbox(m.selectedTeam, m.selectedMember.Name)
			m.mode = agentInboxDetail
		}
	case "e":
		// Edit team config
		home, _ := os.UserHomeDir()
		configPath := filepath.Join(home, ".claude", "teams", m.selectedTeam, "config.json")
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		return m, tea.ExecProcess(exec.Command(editor, configPath), func(err error) tea.Msg {
			return agentEditorDoneMsg{err: err}
		})
	case "o":
		// Open teammate's session in a new tmux pane
		if m.selectedMember != nil && m.selectedMember.AgentType != "lead" {
			openTeammatePane(m.selectedTeam, m.selectedMember)
		}
	}
	return m, nil
}

func (m agentModel) updateInboxDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = agentMemberDetail
	case "e":
		// Edit inbox file
		if m.selectedMember != nil {
			home, _ := os.UserHomeDir()
			inboxPath := filepath.Join(home, ".claude", "teams", m.selectedTeam, "inboxes", m.selectedMember.Name+".json")
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			return m, tea.ExecProcess(exec.Command(editor, inboxPath), func(err error) tea.Msg {
				return agentEditorDoneMsg{err: err}
			})
		}
	}
	return m, nil
}

var (
	agentTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	agentDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	agentHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Bold(true)
	agentNormalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0"))
	agentCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	agentHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
	agentLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	agentValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0"))
)

func (m agentModel) View() tea.View {
	var v tea.View
	switch m.mode {
	case agentMemberDetail:
		v = tea.NewView(m.viewMemberDetail())
	case agentInboxDetail:
		v = tea.NewView(m.viewInbox())
	default:
		v = tea.NewView(m.viewOverview())
	}
	v.AltScreen = true
	return v
}

func (m agentModel) viewOverview() string {
	var b strings.Builder

	b.WriteString(agentTitleStyle.Render("🤖 Agents & Teams") + "\n")
	b.WriteString(agentDimStyle.Render("─────────────────────────────") + "\n")

	if len(m.items) == 0 {
		b.WriteString("\n" + agentDimStyle.Render("  No teams or subagents for this project") + "\n")
	}

	for i, item := range m.items {
		switch item.kind {
		case "team-header":
			cfg, _ := loadTeamConfig(item.team)
			label := item.team
			if cfg != nil && cfg.Description != "" {
				label = item.team + " — " + agentDimStyle.Render(truncate(cfg.Description, 30))
			}
			b.WriteString("\n" + agentHeaderStyle.Render("  ⚙ "+label) + "\n")

		case "member":
			cursor := "  "
			if i == m.cursor {
				cursor = "▸ "
			}
			name := item.member.Name
			agentType := item.member.AgentType
			if agentType == "" {
				agentType = "agent"
			}

			if i == m.cursor {
				b.WriteString(agentCursorStyle.Render(fmt.Sprintf("%s● %s", cursor, name)) +
					" " + agentDimStyle.Render(agentType) + "\n")
			} else {
				b.WriteString(fmt.Sprintf("  ● %s %s\n",
					agentNormalStyle.Render(name),
					agentDimStyle.Render(agentType)))
			}

		case "subagent-header":
			b.WriteString("\n" + agentTitleStyle.Render("  🔀 Subagents") + "\n")

		case "subagent":
			cursor := "  "
			if i == m.cursor {
				cursor = "▸ "
			}
			sa := item.subagent
			idShort := sa.ID
			if len(idShort) > 10 {
				idShort = idShort[:10]
			}
			indicator := "○"
			if time.Since(sa.ModTime) < 30*time.Second {
				indicator = "●"
			}
			ago := timeAgoShort(sa.ModTime)

			if i == m.cursor {
				b.WriteString(agentCursorStyle.Render(fmt.Sprintf("%s%s %s", cursor, indicator, idShort)) +
					" " + agentDimStyle.Render(ago) + "\n")
			} else {
				b.WriteString(fmt.Sprintf("  %s %s %s\n",
					agentDimStyle.Render(indicator),
					agentNormalStyle.Render(idShort),
					agentDimStyle.Render(ago)))
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(agentHintStyle.Render("  ↑↓ nav  ⏎ detail  d close") + "\n")
	return b.String()
}

func (m agentModel) viewMemberDetail() string {
	if m.selectedMember == nil {
		return "No member selected"
	}
	mem := m.selectedMember
	var b strings.Builder

	b.WriteString(agentTitleStyle.Render(fmt.Sprintf("🤖 %s / %s", m.selectedTeam, mem.Name)) + "\n")
	b.WriteString(agentDimStyle.Render("─────────────────────────────") + "\n\n")

	b.WriteString(agentLabelStyle.Render("  Name:  ") + agentValueStyle.Render(mem.Name) + "\n")
	b.WriteString(agentLabelStyle.Render("  Type:  ") + agentValueStyle.Render(mem.AgentType) + "\n")
	if mem.Model != "" {
		b.WriteString(agentLabelStyle.Render("  Model: ") + agentValueStyle.Render(mem.Model) + "\n")
	}
	if mem.Color != "" {
		b.WriteString(agentLabelStyle.Render("  Color: ") + agentValueStyle.Render(mem.Color) + "\n")
	}
	if mem.CWD != "" {
		b.WriteString(agentLabelStyle.Render("  CWD:   ") + agentValueStyle.Render(mem.CWD) + "\n")
	}

	if mem.Prompt != "" {
		b.WriteString("\n" + agentLabelStyle.Render("  Prompt:") + "\n")
		width := 50
		if m.width > 10 {
			width = m.width - 6
		}
		for _, line := range wrapText(mem.Prompt, width) {
			b.WriteString(agentValueStyle.Render("    "+line) + "\n")
		}
	}

	// Count inbox messages
	inbox := loadInbox(m.selectedTeam, mem.Name)
	unread := 0
	for _, msg := range inbox {
		if !msg.Read {
			unread++
		}
	}
	b.WriteString(fmt.Sprintf("\n  %s %d messages",
		agentLabelStyle.Render("Inbox:"),
		len(inbox)))
	if unread > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Render(
			fmt.Sprintf(" (%d unread)", unread)))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	hint := "  esc back  i inbox  e edit  o open pane  d close"
	if mem.AgentType == "lead" {
		hint = "  esc back  i inbox  e edit  d close"
	}
	b.WriteString(agentHintStyle.Render(hint) + "\n")
	return b.String()
}

func (m agentModel) viewInbox() string {
	var b strings.Builder

	name := ""
	if m.selectedMember != nil {
		name = m.selectedMember.Name
	}
	b.WriteString(agentTitleStyle.Render(fmt.Sprintf("📨 %s / %s", m.selectedTeam, name)) + "\n")
	b.WriteString(agentDimStyle.Render("─────────────────────────────") + "\n\n")

	if len(m.inbox) == 0 {
		b.WriteString(agentDimStyle.Render("  (no messages)") + "\n")
	} else {
		for _, msg := range m.inbox {
			ago := timeAgoShort(msg.time())
			readMark := " "
			if !msg.Read {
				readMark = "•"
			}

			from := agentLabelStyle.Render(msg.From + ":")
			text := msg.displayText()

			b.WriteString(fmt.Sprintf("  %s %s %s\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Render(readMark),
				from,
				agentDimStyle.Render(ago)))

			// Show full message text, wrapped
			width := 46
			if m.width > 10 {
				width = m.width - 8
			}
			for _, line := range wrapText(text, width) {
				b.WriteString(agentValueStyle.Render("      "+line) + "\n")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString(agentHintStyle.Render("  esc back  e edit inbox  d close") + "\n")
	return b.String()
}

func timeAgoShort(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// openTeammatePane opens a teammate's claude session in a new tmux split pane
func openTeammatePane(teamName string, member *teamMember) {
	claudeBin := claudeBinary()
	cwd := member.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Launch claude with --resume to connect to the teammate's session
	// The teammate's agentId can be used to find their session
	cmd := fmt.Sprintf("cd %s && %s --teammate-mode in-process",
		shellQuote(cwd), shellQuote(claudeBin))

	tmuxExec("split-window", "-h", "-l", "50%", "-c", cwd, cmd)
}

func runAgentViewer() {
	projectDir := os.Getenv("CLAUDEBAR_CWD")
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	m := newAgentModel(projectDir)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
