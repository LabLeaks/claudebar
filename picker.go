package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// pickerResult is what the TUI returns after the user makes a choice
type pickerResult struct {
	action  string // "attach" or "new"
	session string // session name (if attach)
}

type pickerModel struct {
	matching []sessionInfo
	others   []sessionInfo
	items    []pickerItem // flattened list of selectable items
	cursor   int
	dirName  string
	result   *pickerResult
	quitting bool
}

type pickerItem struct {
	label     string
	session   string // empty for "new session" item
	isNew     bool
	isHeader  bool
	dimDetail string
}

func newPicker(matching, others []sessionInfo, dirName string) pickerModel {
	var items []pickerItem

	if len(matching) > 0 {
		items = append(items, pickerItem{isHeader: true, label: fmt.Sprintf("Sessions for %s", dirName)})
		for _, s := range matching {
			items = append(items, pickerItem{
				label:     s.Name,
				session:   s.Name,
				dimDetail: s.Ago,
			})
		}
	}

	if len(others) > 0 {
		items = append(items, pickerItem{isHeader: true, label: "Other sessions"})
		for _, s := range others {
			items = append(items, pickerItem{
				label:     s.Name,
				session:   s.Name,
				dimDetail: s.Ago,
			})
		}
	}

	items = append(items, pickerItem{isHeader: true, label: ""}) // spacer
	items = append(items, pickerItem{
		label: "New session",
		isNew: true,
	})

	// Set cursor to first selectable item
	cursor := 0
	for i, item := range items {
		if !item.isHeader {
			cursor = i
			break
		}
	}

	return pickerModel{
		matching: matching,
		others:   others,
		items:    items,
		cursor:   cursor,
		dirName:  dirName,
	}
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			m.moveCursor(-1)

		case "down", "j":
			m.moveCursor(1)

		case "enter":
			item := m.items[m.cursor]
			if item.isNew {
				m.result = &pickerResult{action: "new"}
			} else {
				m.result = &pickerResult{action: "attach", session: item.session}
			}
			return m, tea.Quit

		case "n":
			m.result = &pickerResult{action: "new"}
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *pickerModel) moveCursor(dir int) {
	for {
		m.cursor += dir
		if m.cursor < 0 {
			m.cursor = len(m.items) - 1
		}
		if m.cursor >= len(m.items) {
			m.cursor = 0
		}
		if !m.items[m.cursor].isHeader {
			break
		}
	}
}

var (
	titleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).MarginTop(1)
	normalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	newStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88"))
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).MarginTop(1)
)

func (m pickerModel) View() tea.View {
	if m.quitting && m.result == nil {
		return tea.NewView("")
	}
	if m.result != nil {
		return tea.NewView("")
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render(" claudebar"))
	b.WriteString("\n")

	for i, item := range m.items {
		if item.isHeader {
			if item.label == "" {
				b.WriteString("\n")
			} else {
				b.WriteString(headerStyle.Render("  " + item.label))
				b.WriteString("\n")
			}
			continue
		}

		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		if item.isNew {
			if i == m.cursor {
				b.WriteString(activeStyle.Render(cursor+"+ New session") + "\n")
			} else {
				b.WriteString(newStyle.Render(cursor+"+ New session") + "\n")
			}
			continue
		}

		name := item.label
		detail := ""
		if item.dimDetail != "" {
			detail = dimStyle.Render(" (" + item.dimDetail + ")")
		}

		if i == m.cursor {
			b.WriteString(activeStyle.Render(cursor+name) + detail + "\n")
		} else {
			b.WriteString(normalStyle.Render(cursor+name) + detail + "\n")
		}
	}

	b.WriteString(hintStyle.Render("  ↑↓ navigate  ⏎ select  n new  esc quit"))
	b.WriteString("\n")

	return tea.NewView(b.String())
}
