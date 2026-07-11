package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type settingsFieldIndex int

const (
	fieldModelName settingsFieldIndex = iota
	fieldRootAgent
	fieldProvider
	fieldWorkspaceRoot
	fieldGeminiAPIKey
	fieldOpenRouterAPIKey
	fieldCount
)

var settingsFieldLabels = [fieldCount]string{
	fieldModelName:        "Model Name",
	fieldRootAgent:        "Root Agent",
	fieldProvider:         "Provider (gemini|openrouter)",
	fieldWorkspaceRoot:    "Workspace Root",
	fieldGeminiAPIKey:     "Gemini API Key (blank = unchanged)",
	fieldOpenRouterAPIKey: "OpenRouter API Key (blank = unchanged)",
}

// settingsTabModel is the F4 tab: a live-editable view of
// GET/PATCH /botson/settings. Secret fields start blank rather than
// pre-filled with the server's masked "******" -- "blank on save means
// unchanged" is the rule, so leaving a key field alone can never
// accidentally overwrite the real key with the literal string "******".
// Host/Port are shown read-only: PATCH /botson/settings doesn't accept
// them (see internal/networking/api.SettingsSetRequest).
type settingsTabModel struct {
	ctx    context.Context
	client *client

	inputs [fieldCount]textinput.Model
	focus  settingsFieldIndex

	hostPort string

	loaded  bool
	loading bool
	saving  bool
	saved   bool
	note    string
	err     error
}

func newSettingsTab(ctx context.Context, c *client) settingsTabModel {
	var inputs [fieldCount]textinput.Model
	for i := range inputs {
		ti := textinput.New()
		ti.Prompt = "" // the focused-field "> " marker is drawn on the label line instead (see View) -- textinput's own default prompt would otherwise make every field look focused
		if settingsFieldIndex(i) == fieldGeminiAPIKey || settingsFieldIndex(i) == fieldOpenRouterAPIKey {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		inputs[i] = ti
	}
	inputs[fieldModelName].Focus()
	return settingsTabModel{ctx: ctx, client: c, inputs: inputs}
}

func (m settingsTabModel) Init() tea.Cmd { return nil }

func (m *settingsTabModel) SetSize(width, _ int) {
	w := max(width-4, 10)
	for i := range m.inputs {
		m.inputs[i].Width = w
	}
}

// populateFromSettings fills the non-secret fields from a server reply.
// Secret fields are left as-is (blank on first load; whatever the user
// last typed survives a save, since the server never echoes real secrets
// back -- see settingsPayload's doc comment).
func (m *settingsTabModel) populateFromSettings(s *settingsPayload) {
	m.inputs[fieldModelName].SetValue(s.ModelName)
	m.inputs[fieldRootAgent].SetValue(s.RootAgent)
	m.inputs[fieldProvider].SetValue(s.Provider)
	m.inputs[fieldWorkspaceRoot].SetValue(s.WorkspaceRoot)
	m.hostPort = fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// settingsLoadedMsg carries the result of a GetSettings call.
type settingsLoadedMsg struct {
	settings *settingsPayload
	err      error
}

// settingsSavedMsg carries the result of an UpdateSettings call.
type settingsSavedMsg struct {
	settings *settingsPayload
	err      error
}

func (m settingsTabModel) load() tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		s, err := c.GetSettings(ctx)
		return settingsLoadedMsg{settings: s, err: err}
	}
}

// save builds a patch containing only the fields the user actually typed
// into (trimmed-empty means "leave unchanged" for every field, including
// the two secrets).
func (m settingsTabModel) save() tea.Cmd {
	var patch settingsPatch
	if v := strings.TrimSpace(m.inputs[fieldModelName].Value()); v != "" {
		patch.ModelName = &v
	}
	if v := strings.TrimSpace(m.inputs[fieldRootAgent].Value()); v != "" {
		patch.RootAgent = &v
	}
	if v := strings.TrimSpace(m.inputs[fieldProvider].Value()); v != "" {
		patch.Provider = &v
	}
	if v := strings.TrimSpace(m.inputs[fieldWorkspaceRoot].Value()); v != "" {
		patch.WorkspaceRoot = &v
	}
	if v := m.inputs[fieldGeminiAPIKey].Value(); v != "" {
		if patch.ProviderKeys == nil {
			patch.ProviderKeys = &providerKeysPatch{}
		}
		patch.ProviderKeys.Gemini = &v
	}
	if v := m.inputs[fieldOpenRouterAPIKey].Value(); v != "" {
		if patch.ProviderKeys == nil {
			patch.ProviderKeys = &providerKeysPatch{}
		}
		patch.ProviderKeys.OpenRouter = &v
	}

	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		s, err := c.UpdateSettings(ctx, patch)
		return settingsSavedMsg{settings: s, err: err}
	}
}

func (m settingsTabModel) Update(msg tea.Msg) (settingsTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		m.loading, m.loaded = false, true
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.populateFromSettings(msg.settings)
		return m, nil

	case settingsSavedMsg:
		m.saving = false
		if msg.err != nil {
			m.err = msg.err
			m.saved = false
			return m, nil
		}
		m.err = nil
		m.saved = true
		m.note = msg.settings.Note
		// Re-populate from the server's reply -- clears the secret
		// fields back to blank ("unchanged" state), same as a fresh
		// load, and reflects any server-side normalization.
		m.populateFromSettings(msg.settings)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m settingsTabModel) handleKey(msg tea.KeyMsg) (settingsTabModel, tea.Cmd) {
	switch msg.String() {
	case "ctrl+r":
		if m.saving {
			return m, nil
		}
		m.loading = true
		m.saved = false
		m.err = nil
		return m, m.load()

	case "ctrl+s":
		if m.saving {
			return m, nil
		}
		m.saving = true
		m.saved = false
		m.err = nil
		return m, m.save()

	case "tab", "down":
		m.inputs[m.focus].Blur()
		m.focus = (m.focus + 1) % fieldCount
		m.inputs[m.focus].Focus()
		return m, nil

	case "shift+tab", "up":
		m.inputs[m.focus].Blur()
		m.focus = (m.focus - 1 + fieldCount) % fieldCount
		m.inputs[m.focus].Focus()
		return m, nil
	}

	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m settingsTabModel) View() string {
	var b strings.Builder
	for i := settingsFieldIndex(0); i < fieldCount; i++ {
		label := "  " + settingsFieldLabels[i]
		if i == m.focus {
			label = selectedItemStyle.Render("> " + settingsFieldLabels[i])
		}
		fmt.Fprintf(&b, "%s\n%s\n\n", label, m.inputs[i].View())
	}
	if m.hostPort != "" {
		fmt.Fprintf(&b, "%s\n\n", dimStyle.Render("API bind address: "+m.hostPort+" (not editable here)"))
	}

	var status string
	switch {
	case m.err != nil:
		status = errStyle.Render("error: " + m.err.Error())
	case m.saving:
		status = dimStyle.Render("saving...")
	case m.loading:
		status = dimStyle.Render("loading...")
	case m.saved:
		status = successStyle.Render("saved.")
		if m.note != "" {
			status += "\n" + promptStyle.Render(m.note)
		}
	default:
		status = dimStyle.Render("tab/shift+tab: move field  ctrl+s: save  ctrl+r: refresh")
	}
	b.WriteString(status)
	return b.String()
}
