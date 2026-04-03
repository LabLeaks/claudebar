package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// wizardStep tracks which form field the user is editing.
type wizardStep int

const (
	stepName wizardStep = iota
	stepProvider
	stepAPIKey
	stepDefaultModel
	stepThinkModel
	stepContext
	stepConfirm
)

var stepLabels = []string{
	"Config name",
	"Provider",
	"API key",
	"Default model",
	"Think model (optional)",
	"Context window",
	"Confirm",
}

// routerWizardResult is what the TUI returns on completion.
type routerWizardResult struct {
	name   string
	config *routerConfig
}

type routerWizardModel struct {
	step      wizardStep
	fields    [5]string // name, provider, apiKey, defaultModel, thinkModel
	context1m bool      // whether to enable 1M context
	cursor    int       // for provider/context selection
	provider  []string  // sorted provider names
	result    *routerWizardResult
	quitting  bool
	err       string // validation error for current step
}

func newRouterWizard() routerWizardModel {
	providers := make([]string, 0, len(knownProviders))
	for name := range knownProviders {
		providers = append(providers, name)
	}
	sort.Strings(providers)

	return routerWizardModel{
		step:     stepName,
		provider: providers,
	}
}

func (m routerWizardModel) Init() tea.Cmd {
	return nil
}

func (m routerWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Handle paste into text input fields
		if m.step != stepProvider && m.step != stepConfirm {
			fieldIdx := int(m.step)
			m.fields[fieldIdx] += msg.Content
			m.err = ""
		}
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()

		// Global quit
		if key == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}

		// Esc goes back a step or quits
		if key == "esc" {
			if m.step == stepName {
				m.quitting = true
				return m, tea.Quit
			}
			m.step--
			m.err = ""
			return m, nil
		}

		switch m.step {
		case stepProvider:
			return m.updateProviderSelect(key)
		case stepContext:
			return m.updateContextSelect(key)
		case stepConfirm:
			return m.updateConfirm(key)
		default:
			return m.updateTextInput(msg)
		}
	}
	return m, nil
}

func (m routerWizardModel) updateTextInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Steps map directly to field indices: 0=name, 1=provider, 2=apiKey, 3=defaultModel, 4=thinkModel
	// Provider (1) is handled by updateProviderSelect, so this is never called for it.
	fieldIdx := int(m.step)

	key := msg.String()
	switch key {
	case "enter":
		if err := m.validateStep(); err != "" {
			m.err = err
			return m, nil
		}
		m.err = ""
		m.step++
		return m, nil
	case "backspace":
		if len(m.fields[fieldIdx]) > 0 {
			m.fields[fieldIdx] = m.fields[fieldIdx][:len(m.fields[fieldIdx])-1]
		}
		m.err = ""
	default:
		// msg.Text handles both single keystrokes and paste (multi-char)
		if msg.Text != "" {
			m.fields[fieldIdx] += msg.Text
			m.err = ""
		}
	}
	return m, nil
}

func (m routerWizardModel) updateProviderSelect(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.provider)-1 {
			m.cursor++
		}
	case "enter":
		m.fields[1] = m.provider[m.cursor]
		m.step++
		return m, nil
	}
	return m, nil
}

func (m routerWizardModel) updateContextSelect(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "down", "k", "j", "tab":
		m.context1m = !m.context1m
	case "enter":
		m.step++
		return m, nil
	}
	return m, nil
}

func (m routerWizardModel) updateConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "y":
		m.result = m.buildResult()
		return m, tea.Quit
	case "n":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m routerWizardModel) validateStep() string {
	switch m.step {
	case stepName:
		name := strings.TrimSpace(m.fields[0])
		if name == "" {
			return "name cannot be empty"
		}
		if strings.ContainsAny(name, " /\\\"'") {
			return "name cannot contain spaces or special characters"
		}
	case stepAPIKey:
		key := strings.TrimSpace(m.fields[2])
		if key == "" {
			return "API key cannot be empty (use $ENV_VAR for env reference)"
		}
	case stepDefaultModel:
		model := strings.TrimSpace(m.fields[3])
		if model == "" {
			return "default model is required"
		}
	}
	return ""
}

func (m routerWizardModel) buildResult() *routerWizardResult {
	name := strings.TrimSpace(m.fields[0])
	provider := m.fields[1]
	apiKey := strings.TrimSpace(m.fields[2])
	defaultModel := strings.TrimSpace(m.fields[3])
	thinkModel := strings.TrimSpace(m.fields[4])

	// Build model slots
	models := map[string]string{
		"default": provider + "," + defaultModel,
	}
	if thinkModel != "" {
		models["think"] = provider + "," + thinkModel
	}

	// Sensible default transformers per provider
	transformers := defaultTransformers(provider)

	return &routerWizardResult{
		name: name,
		config: &routerConfig{
			Provider:     provider,
			APIKey:       apiKey,
			Models:       models,
			Transformers: transformers,
			Context1M:    m.context1m,
		},
	}
}

// defaultTransformers returns sensible transformer defaults for a provider.
func defaultTransformers(provider string) []interface{} {
	switch provider {
	case "openrouter":
		return []interface{}{"openrouter", "enhancetool", "cleancache"}
	case "deepseek":
		return []interface{}{"deepseek", "enhancetool", "cleancache", "reasoning"}
	default:
		return []interface{}{"enhancetool", "cleancache"}
	}
}

// --- styles (reuse existing where possible, add wizard-specific ones) ---

var (
	wizardTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Bold(true)
	wizardLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	wizardValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0"))
	wizardInputStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88"))
	wizardErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
	wizardDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	wizardHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
)

func (m routerWizardModel) View() tea.View {
	if m.quitting && m.result == nil {
		return tea.NewView("")
	}
	if m.result != nil {
		return tea.NewView("")
	}

	var b strings.Builder

	b.WriteString(wizardTitleStyle.Render(" New Router Config"))
	b.WriteString("\n\n")

	// Show completed fields
	type completedField struct {
		label string
		value string
	}
	var completed []completedField
	if m.step > stepName {
		completed = append(completed, completedField{"Name", m.fields[0]})
	}
	if m.step > stepProvider {
		completed = append(completed, completedField{"Provider", m.fields[1]})
	}
	if m.step > stepAPIKey {
		val := m.fields[2]
		if !strings.HasPrefix(val, "$") && len(val) > 8 {
			val = val[:4] + strings.Repeat("·", len(val)-8) + val[len(val)-4:]
		}
		completed = append(completed, completedField{"API key", val})
	}
	if m.step > stepDefaultModel {
		completed = append(completed, completedField{"Default model", m.fields[3]})
	}
	if m.step > stepThinkModel {
		val := m.fields[4]
		if val == "" {
			val = "(skipped)"
		}
		completed = append(completed, completedField{"Think model", val})
	}
	if m.step > stepContext {
		ctx := "200K (standard)"
		if m.context1m {
			ctx = "1M tokens"
		}
		completed = append(completed, completedField{"Context", ctx})
	}
	for _, f := range completed {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			wizardLabelStyle.Render(fmt.Sprintf("%-14s", f.label)),
			wizardValueStyle.Render(f.value)))
	}

	// Current step
	switch m.step {
	case stepProvider:
		b.WriteString("\n")
		b.WriteString(wizardLabelStyle.Render("  Provider"))
		b.WriteString("\n")
		for i, p := range m.provider {
			cursor := "  "
			if i == m.cursor {
				cursor = "▸ "
				b.WriteString(activeStyle.Render("  "+cursor+p) + "\n")
			} else {
				b.WriteString(normalStyle.Render("  "+cursor+p) + "\n")
			}
		}

	case stepContext:
		b.WriteString("\n")
		b.WriteString(wizardLabelStyle.Render("  Context window"))
		b.WriteString("\n")
		opts := []struct{ label string; val bool }{
			{"1M tokens (large-context models like Qwen 3.6+)", true},
			{"200K tokens (standard)", false},
		}
		for _, opt := range opts {
			cursor := "  "
			if opt.val == m.context1m {
				cursor = "▸ "
				b.WriteString(activeStyle.Render("  "+cursor+opt.label) + "\n")
			} else {
				b.WriteString(normalStyle.Render("  "+cursor+opt.label) + "\n")
			}
		}
		b.WriteString(wizardDimStyle.Render("  arrows to toggle, enter to confirm") + "\n")

	case stepConfirm:
		b.WriteString("\n")
		// Show transformers that will be applied
		provider := m.fields[1]
		transformers := defaultTransformers(provider)
		tNames := make([]string, len(transformers))
		for i, t := range transformers {
			tNames[i] = fmt.Sprint(t)
		}
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			wizardLabelStyle.Render(fmt.Sprintf("%-14s", "Transformers")),
			wizardDimStyle.Render(strings.Join(tNames, ", "))))
		b.WriteString("\n")
		b.WriteString(wizardInputStyle.Render("  Save this config? (y/enter to save, n/esc to cancel)"))
		b.WriteString("\n")

	default:
		// Text input step — step index == field index
		fieldIdx := int(m.step)

		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s\n", wizardLabelStyle.Render(stepLabels[m.step])))

		input := m.fields[fieldIdx]
		// Mask API key input
		display := input
		if m.step == stepAPIKey && !strings.HasPrefix(input, "$") && len(input) > 4 {
			display = input[:4] + strings.Repeat("·", len(input)-4)
		}
		b.WriteString(fmt.Sprintf("  %s%s\n", wizardInputStyle.Render(display), wizardInputStyle.Render("█")))

		// Hints per step
		switch m.step {
		case stepName:
			b.WriteString(wizardDimStyle.Render("  e.g., openrouter-qwen") + "\n")
		case stepAPIKey:
			b.WriteString(wizardDimStyle.Render("  paste a key or use $ENV_VAR") + "\n")
			b.WriteString(wizardDimStyle.Render("  e.g., $OPENROUTER_KEY") + "\n")
		case stepDefaultModel:
			b.WriteString(wizardDimStyle.Render("  the model ID from your provider") + "\n")
			b.WriteString(wizardDimStyle.Render("  e.g., qwen/qwen-coder-plus:free") + "\n")
		case stepThinkModel:
			b.WriteString(wizardDimStyle.Render("  optional reasoning model, enter to skip") + "\n")
			b.WriteString(wizardDimStyle.Render("  e.g., qwen/qwq-32b:free") + "\n")
		}
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(wizardErrStyle.Render("  ✗ "+m.err) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(wizardHintStyle.Render("  ⏎ next  esc back  ctrl+c quit"))
	b.WriteString("\n")

	return tea.NewView(b.String())
}

func (m routerWizardModel) displayValues() []string {
	return []string{
		m.fields[0], // name
		m.fields[1], // provider
		m.fields[2], // apiKey
		m.fields[3], // defaultModel
		m.fields[4], // thinkModel
	}
}

// runRouterWizard launches the wizard TUI and saves the result to config.
func runRouterWizard() {
	model := newRouterWizard()
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		return
	}

	result := finalModel.(routerWizardModel).result
	if result == nil {
		return // user cancelled
	}

	cfg := loadConfig()
	if cfg.RouterConfigs == nil {
		cfg.RouterConfigs = make(map[string]*routerConfig)
	}
	cfg.RouterConfigs[result.name] = result.config
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		return
	}

	// Regenerate CCR config if CCR is running
	if _, alive := ccrRunning(); alive {
		generateCCRConfig(cfg)
	}

	tmuxExec("display-message", fmt.Sprintf("Router config %q saved", result.name))
}
