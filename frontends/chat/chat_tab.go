package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"botson/internal/networking/adkwire"
)

// pendingConfirmation holds the state needed to answer an
// adk_request_confirmation call the model is currently waiting on -- see
// AGENTS.md's "HITL confirmation wire protocol".
type pendingConfirmation struct {
	callID string
	hint   string
}

// chatTabModel is the default (F1) tab: the interactive chat view.
// agent/user/sessionID are mutable rather than fixed at construction, so
// switchSessionMsg (from the Sessions/Stats tabs) and switchAgentMsg (from
// the Agents tab) can repoint an already-running chat tab without
// restarting the program.
//
// Known limitation: resuming a session via switchSessionMsg that has a
// genuinely pending, unanswered HITL confirmation won't show the y/n
// prompt retroactively -- GetSession's SessionEventSummary only carries
// already-reduced display text, not enough to reconstruct
// adk_request_confirmation state. The pending prompt reappears correctly
// on this session's *next* turn; only the resume-time replay is affected.
type chatTabModel struct {
	ctx    context.Context
	client *client

	agent     string
	user      string
	sessionID string
	autoMode  bool

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	ready   bool // SetSize has been called at least once
	waiting bool // a turn (or other async action) is in flight
	pending *pendingConfirmation
	err     error

	history []string // rendered lines, newest last
}

func newChatTab(ctx context.Context, c *client, agent, user, sessionID string) chatTabModel {
	ti := textinput.New()
	ti.Placeholder = "message " + agent + "..."
	ti.Focus()
	ti.CharLimit = 4000

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return chatTabModel{
		ctx:       ctx,
		client:    c,
		agent:     agent,
		user:      user,
		sessionID: sessionID,
		input:     ti,
		spinner:   sp,
	}
}

func (m chatTabModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

// SetSize lays out the viewport/input for the given content area (root
// already subtracts its own tab-bar/help-bar chrome before calling this).
func (m *chatTabModel) SetSize(width, height int) {
	const statusHeight, footerHeight = 1, 1
	if !m.ready {
		m.viewport = viewport.New(width, height-statusHeight-footerHeight)
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.ready = true
	} else {
		m.viewport.Width = width
		m.viewport.Height = height - statusHeight - footerHeight
	}
	m.input.Width = width - 2
}

// turnResultMsg is delivered when a RunTurn call (from a submitted user
// message or a HITL confirmation answer) completes.
type turnResultMsg struct {
	events []adkwire.Event
	err    error
}

// sessionLoadedMsg is delivered when GetSession completes, in response to
// a switchSessionMsg.
type sessionLoadedMsg struct {
	agent, user, sessionID string
	detail                 *sessionDetail
	err                    error
}

// newSessionMsg is delivered when CreateSession completes, in response to
// ctrl+n or a switchAgentMsg.
type newSessionMsg struct {
	agent, user, sessionID string
	err                    error
}

// autoModeSetMsg is delivered when SetSessionAutoMode completes.
type autoModeSetMsg struct {
	enabled bool
	err     error
}

// submitMessage sends text as a new user turn.
func (m chatTabModel) submitMessage(text string) tea.Cmd {
	req := adkwire.RunAgentRequest{
		AppName:   m.agent,
		UserID:    m.user,
		SessionID: m.sessionID,
		NewMessage: genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: text}},
		},
	}
	return m.runTurn(req)
}

// answerConfirmation sends the human's decision back on the pending
// adk_request_confirmation call.
func (m chatTabModel) answerConfirmation(confirmed bool) tea.Cmd {
	if m.pending == nil {
		return nil
	}
	req := adkwire.RunAgentRequest{
		AppName:   m.agent,
		UserID:    m.user,
		SessionID: m.sessionID,
		NewMessage: genai.Content{
			Role: "user",
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					ID:       m.pending.callID,
					Name:     toolconfirmation.FunctionCallName,
					Response: map[string]any{"confirmed": confirmed},
				},
			}},
		},
	}
	return m.runTurn(req)
}

func (m chatTabModel) runTurn(req adkwire.RunAgentRequest) tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		events, err := c.RunTurn(ctx, req)
		return turnResultMsg{events: events, err: err}
	}
}

// loadSession fetches full history for a session switch.
func (m chatTabModel) loadSession(agent, user, sessionID string) tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		detail, err := c.GetSession(ctx, agent, user, sessionID)
		return sessionLoadedMsg{agent: agent, user: user, sessionID: sessionID, detail: detail, err: err}
	}
}

// startNewSession creates a fresh session against agent for the current
// user.
func (m chatTabModel) startNewSession(agent string) tea.Cmd {
	c, ctx, user := m.client, m.ctx, m.user
	sessionID := uuid.NewString()
	return func() tea.Msg {
		err := c.CreateSession(ctx, agent, user, sessionID)
		return newSessionMsg{agent: agent, user: user, sessionID: sessionID, err: err}
	}
}

func (m chatTabModel) toggleAutoMode() tea.Cmd {
	c, ctx := m.client, m.ctx
	agent, user, sessionID := m.agent, m.user, m.sessionID
	enabled := !m.autoMode
	return func() tea.Msg {
		err := c.SetSessionAutoMode(ctx, agent, user, sessionID, enabled)
		return autoModeSetMsg{enabled: enabled, err: err}
	}
}

func (m chatTabModel) Update(msg tea.Msg) (chatTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case switchSessionMsg:
		m.waiting = true
		m.err = nil
		return m, m.loadSession(msg.agent, msg.user, msg.sessionID)

	case switchAgentMsg:
		m.waiting = true
		m.err = nil
		return m, m.startNewSession(msg.agent)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case turnResultMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		lines, pending := renderEvents(msg.events)
		m.history = append(m.history, lines...)
		m.pending = pending
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		return m, nil

	case sessionLoadedMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.agent, m.user, m.sessionID = msg.agent, msg.user, msg.sessionID
		m.pending = nil
		m.history = nil
		for _, ev := range msg.detail.Events {
			if ev.Text == "" {
				continue
			}
			m.history = append(m.history, fmt.Sprintf("%s: %s", ev.Author, ev.Text))
		}
		m.autoMode, _ = msg.detail.State["botson:autoMode"].(bool)
		m.input.Placeholder = "message " + m.agent + "..."
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		return m, nil

	case newSessionMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.agent, m.user, m.sessionID = msg.agent, msg.user, msg.sessionID
		m.pending = nil
		m.autoMode = false
		m.history = nil
		m.input.Placeholder = "message " + m.agent + "..."
		m.viewport.SetContent("")
		return m, nil

	case autoModeSetMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.autoMode = msg.enabled
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m chatTabModel) handleKey(msg tea.KeyMsg) (chatTabModel, tea.Cmd) {
	if m.pending != nil {
		switch strings.ToLower(msg.String()) {
		case "y":
			cmd := m.answerConfirmation(true)
			m.waiting = true
			m.pending = nil
			return m, cmd
		case "n", "esc":
			cmd := m.answerConfirmation(false)
			m.waiting = true
			m.pending = nil
			return m, cmd
		}
		return m, nil // swallow everything else (including enter) while a gated action awaits an explicit y/n
	}

	if m.waiting {
		return m, nil // swallow input while a turn/action is in flight
	}

	switch msg.String() {
	case "ctrl+n":
		m.waiting = true
		return m, m.startNewSession(m.agent)
	case "ctrl+a":
		m.waiting = true
		return m, m.toggleAutoMode()
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.history = append(m.history, "you: "+text)
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		m.input.Reset()
		m.waiting = true
		return m, m.submitMessage(text)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m chatTabModel) View() string {
	if !m.ready {
		return "initializing...\n"
	}

	var footer string
	switch {
	case m.pending != nil:
		footer = promptStyle.Render(fmt.Sprintf("%s [y/n] ", m.pending.hint))
	case m.waiting:
		footer = m.spinner.View() + " thinking..."
	default:
		footer = m.input.View()
	}

	var errLine string
	if m.err != nil {
		errLine = errStyle.Render("error: "+m.err.Error()) + "\n"
	}

	status := fmt.Sprintf("— %s (%s) —", m.agent, m.sessionID)
	if m.autoMode {
		status += " " + successStyle.Render("[auto-mode]")
	}

	return fmt.Sprintf("%s\n%s%s\n%s",
		m.viewport.View(),
		errLine,
		dimStyle.Render(status),
		footer,
	)
}
