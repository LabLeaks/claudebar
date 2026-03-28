package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type task struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	ActiveForm  string   `json:"activeForm"`
	Owner       string   `json:"owner"`
	Blocks      []string `json:"blocks"`
	BlockedBy   []string `json:"blockedBy"`
}

func taskListDir(listID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "tasks", listID)
}

func taskListIDForSession(sessionName string) string {
	return "claudebar-" + sessionName
}

func loadTasks(listID string) ([]task, error) {
	dir := taskListDir(listID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tasks []task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t task
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool {
		a, _ := strconv.Atoi(tasks[i].ID)
		b, _ := strconv.Atoi(tasks[j].ID)
		return a < b
	})

	return tasks, nil
}

func saveTask(listID string, t task) error {
	dir := taskListDir(listID)
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, t.ID+".json"), data, 0644)
}

func deleteTask(listID string, t task) error {
	dir := taskListDir(listID)
	return os.Remove(filepath.Join(dir, t.ID+".json"))
}

// --- Bubbletea TUI ---

type taskViewMode int

const (
	taskListView taskViewMode = iota
	taskDetailView
)

type taskModel struct {
	listID   string
	tasks    []task
	cursor   int
	mode     taskViewMode
	width    int
	height   int
	lastLoad time.Time
	message  string // transient status message
}

type tickMsg time.Time

func taskTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func newTaskModel(listID string) taskModel {
	tasks, _ := loadTasks(listID)
	return taskModel{
		listID:   listID,
		tasks:    tasks,
		lastLoad: time.Now(),
	}
}

func (m taskModel) Init() tea.Cmd {
	return taskTick()
}

func (m taskModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if !mainPaneAlive() {
			return m, tea.Quit
		}
		tasks, _ := loadTasks(m.listID)
		m.tasks = tasks
		m.lastLoad = time.Now()
		m.message = ""
		return m, taskTick()

	case editorDoneMsg:
		// Reload tasks after editor closes
		tasks, _ := loadTasks(m.listID)
		m.tasks = tasks
		if msg.err != nil {
			m.message = "editor error"
		} else {
			m.message = "saved"
		}
		return m, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			if m.cursor > 0 {
				m.cursor--
			}
		} else if mouse.Button == tea.MouseWheelDown {
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case taskListView:
			return m.updateList(msg)
		case taskDetailView:
			return m.updateDetail(msg)
		}
	}
	return m, nil
}

func (m taskModel) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
		}

	case "enter":
		if len(m.tasks) > 0 {
			m.mode = taskDetailView
		}

	case "s":
		// Cycle status
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := &m.tasks[m.cursor]
			switch t.Status {
			case "pending":
				t.Status = "in_progress"
			case "in_progress":
				t.Status = "completed"
			case "completed":
				t.Status = "pending"
			default:
				t.Status = "pending"
			}
			saveTask(m.listID, *t)
			m.message = fmt.Sprintf("→ %s", t.Status)
		}

	case "e":
		// Edit task in $EDITOR
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			taskFile := filepath.Join(taskListDir(m.listID), t.ID+".json")
			return m, tea.ExecProcess(editorCmd(taskFile), func(err error) tea.Msg {
				return editorDoneMsg{err: err}
			})
		}

	case "x":
		// Delete task
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			deleteTask(m.listID, t)
			m.tasks = append(m.tasks[:m.cursor], m.tasks[m.cursor+1:]...)
			if m.cursor >= len(m.tasks) && m.cursor > 0 {
				m.cursor--
			}
			m.message = "deleted"
		}
	}
	return m, nil
}

func (m taskModel) updateDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "d", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.mode = taskListView
	case "s":
		if m.cursor < len(m.tasks) {
			t := &m.tasks[m.cursor]
			switch t.Status {
			case "pending":
				t.Status = "in_progress"
			case "in_progress":
				t.Status = "completed"
			case "completed":
				t.Status = "pending"
			}
			saveTask(m.listID, *t)
			m.message = fmt.Sprintf("→ %s", t.Status)
		}
	case "e":
		if m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			taskFile := filepath.Join(taskListDir(m.listID), t.ID+".json")
			return m, tea.ExecProcess(editorCmd(taskFile), func(err error) tea.Msg {
				return editorDoneMsg{err: err}
			})
		}
	}
	return m, nil
}

type editorDoneMsg struct {
	err error
}

func editorCmd(file string) *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	return exec.Command(editor, file)
}

var (
	taskTitleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	taskDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	taskActiveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	taskDoneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	taskHintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
	taskDetailLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	taskDetailValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0"))
	taskMsgStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700"))
)

func (m taskModel) View() tea.View {
	var v tea.View
	switch m.mode {
	case taskDetailView:
		v = tea.NewView(m.viewDetail())
	default:
		v = tea.NewView(m.viewList())
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func statusIcon(status string) string {
	switch status {
	case "in_progress":
		return "⟳"
	case "completed":
		return "✓"
	default:
		return "○"
	}
}

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "in_progress":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700"))
	case "completed":
		return taskDoneStyle
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	}
}

func (m taskModel) viewList() string {
	var b strings.Builder

	b.WriteString(taskTitleStyle.Render("📋 Tasks"))
	if m.message != "" {
		b.WriteString("  " + taskMsgStyle.Render(m.message))
	}
	b.WriteString("\n")
	b.WriteString(taskDimStyle.Render("─────────────────────────────") + "\n")

	if len(m.tasks) == 0 {
		b.WriteString(taskDimStyle.Render("  (no tasks)") + "\n")
	} else {
		// Count stats
		var done, active int
		for _, t := range m.tasks {
			if t.Status == "completed" {
				done++
			}
			if t.Status == "in_progress" {
				active++
			}
		}

		for i, t := range m.tasks {
			cursor := "  "
			if i == m.cursor {
				cursor = "▸ "
			}

			icon := statusIcon(t.Status)
			style := statusStyle(t.Status)
			subject := truncate(t.Subject, 40)

			if i == m.cursor {
				b.WriteString(taskActiveStyle.Render(cursor+icon+" "+subject) + "\n")
			} else {
				b.WriteString(style.Render(cursor+icon+" "+subject) + "\n")
			}
		}

		b.WriteString("\n")
		b.WriteString(taskDimStyle.Render(fmt.Sprintf("  %d total · %d done · %d active",
			len(m.tasks), done, active)) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(taskHintStyle.Render("  ↑↓ nav  ⏎ detail  e edit  s status  x del  d close") + "\n")

	return b.String()
}

func (m taskModel) viewDetail() string {
	if m.cursor >= len(m.tasks) {
		return "No task selected"
	}
	t := m.tasks[m.cursor]

	var b strings.Builder

	b.WriteString(taskTitleStyle.Render("📋 Task Detail"))
	if m.message != "" {
		b.WriteString("  " + taskMsgStyle.Render(m.message))
	}
	b.WriteString("\n")
	b.WriteString(taskDimStyle.Render("─────────────────────────────") + "\n\n")

	b.WriteString(taskDetailLabel.Render("  ID:      ") + taskDetailValue.Render(t.ID) + "\n")
	b.WriteString(taskDetailLabel.Render("  Status:  ") + statusStyle(t.Status).Render(statusIcon(t.Status)+" "+t.Status) + "\n")
	b.WriteString(taskDetailLabel.Render("  Subject: ") + taskDetailValue.Render(t.Subject) + "\n")

	if t.Description != "" {
		b.WriteString("\n")
		b.WriteString(taskDetailLabel.Render("  Description:") + "\n")
		// Word wrap description
		for _, line := range wrapText(t.Description, 40) {
			b.WriteString(taskDetailValue.Render("    "+line) + "\n")
		}
	}

	if t.ActiveForm != "" {
		b.WriteString("\n")
		b.WriteString(taskDetailLabel.Render("  Active:  ") + taskDetailValue.Render(t.ActiveForm) + "\n")
	}

	if t.Owner != "" {
		b.WriteString(taskDetailLabel.Render("  Owner:   ") + taskDetailValue.Render(t.Owner) + "\n")
	}

	if len(t.Blocks) > 0 {
		b.WriteString(taskDetailLabel.Render("  Blocks:  ") + taskDetailValue.Render(strings.Join(t.Blocks, ", ")) + "\n")
	}
	if len(t.BlockedBy) > 0 {
		b.WriteString(taskDetailLabel.Render("  Blocked: ") + taskDetailValue.Render(strings.Join(t.BlockedBy, ", ")) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(taskHintStyle.Render("  esc back  e edit  s status  d close") + "\n")

	return b.String()
}

func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > width {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return lines
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// runTaskViewer launches the interactive task TUI
func runTaskViewer() {
	listID := os.Getenv("CLAUDEBAR_TASK_LIST_ID")
	if listID == "" {
		fmt.Fprintln(os.Stderr, "No task list ID set.")
		os.Exit(1)
	}

	m := newTaskModel(listID)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
