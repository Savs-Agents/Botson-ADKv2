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
// Sessions are created lazily: a freshly-generated sessionID (at startup,
// after ctrl+n, or after switching agent) is never POSTed to the server
// until the user actually does something with it -- sends a message or
// toggles auto-mode. sessionCreated tracks whether that's happened yet.
// Without this, simply opening the TUI (or pressing ctrl+n) and doing
// nothing would leave an empty, never-used session persisted on disk.
// switchSessionMsg is exempt: it resumes a session that already exists
// (it came from GET /botson/sessions), so sessionCreated starts true for
// it.
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

	agent          string
	user           string
	sessionID      string
	sessionCreated bool // has sessionID actually been POSTed to the server yet?
	autoMode       bool

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	width int // full content width, for the status bar/input box's own borders

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

// SetSize lays out the status bar/viewport/input box for the given
// content area (root already subtracts its own tab-bar/help-bar chrome
// before calling this). statusBarHeight/inputBoxHeight must match what
// View() actually renders exactly -- both are fixed regardless of
// content (see statusLine/inputBoxContent, which truncate rather than
// wrap so their content is always exactly one line), so the viewport
// gets a precise, never-overflowing remainder rather than an estimate.
func (m *chatTabModel) SetSize(width, height int) {
	m.width = width
	const statusBarHeight = 2 // 1 content line + 1 bottom border
	const inputBoxHeight = 3  // 1 top border + 1 content line + 1 bottom border
	viewportHeight := max(height-statusBarHeight-inputBoxHeight, 0)

	if !m.ready {
		m.viewport = viewport.New(width, viewportHeight)
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.ready = true
	} else {
		m.viewport.Width = width
		m.viewport.Height = viewportHeight
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
	return m.ensureSessionAndRunTurn(req)
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

// ensureSessionAndRunTurn is runTurn, but first creates the session on the
// server if it hasn't been created yet (see sessionCreated's doc comment
// on chatTabModel). ADK's own POST /api/run requires the session to
// already exist -- it 404s otherwise, it does not auto-create one -- so
// this is the one place that lazy-creation actually has to happen.
func (m chatTabModel) ensureSessionAndRunTurn(req adkwire.RunAgentRequest) tea.Cmd {
	if m.sessionCreated {
		return m.runTurn(req)
	}
	c, ctx := m.client, m.ctx
	agent, user, sessionID := m.agent, m.user, m.sessionID
	return func() tea.Msg {
		if err := c.CreateSession(ctx, agent, user, sessionID); err != nil {
			return turnResultMsg{err: err}
		}
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

// newLocalSession resets chat state to a brand-new, not-yet-created
// session against agent -- purely local, no network call. The session is
// only persisted lazily, on the first real action (see sessionCreated's
// doc comment), so switching to a fresh session (ctrl+n, or picking a
// different agent from the Agents tab) that the user then never actually
// uses leaves nothing behind on disk.
func (m chatTabModel) newLocalSession(agent string) chatTabModel {
	m.agent = agent
	m.sessionID = uuid.NewString()
	m.sessionCreated = false
	m.waiting = false
	m.pending = nil
	m.autoMode = false
	m.history = nil
	m.err = nil
	m.input.Reset()
	m.input.Placeholder = "message " + agent + "..."
	if m.ready {
		m.viewport.SetContent("")
	}
	return m
}

// toggleAutoMode flips auto-mode for the active session, creating the
// session first if it hasn't been created yet (same reasoning as
// ensureSessionAndRunTurn -- PATCH .../autoMode 404s against a session
// that was never actually POSTed).
func (m chatTabModel) toggleAutoMode() tea.Cmd {
	c, ctx := m.client, m.ctx
	agent, user, sessionID := m.agent, m.user, m.sessionID
	enabled := !m.autoMode
	needsCreate := !m.sessionCreated
	return func() tea.Msg {
		if needsCreate {
			if err := c.CreateSession(ctx, agent, user, sessionID); err != nil {
				return autoModeSetMsg{err: err}
			}
		}
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
		return m.newLocalSession(msg.agent), nil

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
		m.sessionCreated = true
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
		m.sessionCreated = true // resumed from GET /botson/sessions -- it already exists
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

	case autoModeSetMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.sessionCreated = true
		m.autoMode = msg.enabled
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// scrollKeys are checked first, ahead of the pending-confirmation and
// waiting guards below, so the transcript can be scrolled at any time --
// including while a turn is in flight or a confirmation is pending, both
// reasonable times to want to scroll back up and re-read context. These
// are deliberately a narrow, hand-picked set (not viewport's own
// DefaultKeyMap, which binds plain letters like "j"/"k"/"f"/"b"/"u"/"d"
// and the arrow keys -- exactly the keys the textinput below needs for
// normal typing and cursor movement) so scrolling can never eat a
// keystroke meant for the input.
func (m *chatTabModel) handleScrollKey(key string) bool {
	switch key {
	case "pgup":
		m.viewport.PageUp()
	case "pgdown":
		m.viewport.PageDown()
	case "ctrl+u":
		m.viewport.HalfPageUp()
	case "ctrl+d":
		m.viewport.HalfPageDown()
	case "home":
		m.viewport.GotoTop()
	case "end":
		m.viewport.GotoBottom()
	default:
		return false
	}
	return true
}

func (m chatTabModel) handleKey(msg tea.KeyMsg) (chatTabModel, tea.Cmd) {
	if m.handleScrollKey(msg.String()) {
		return m, nil
	}

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
		return m.newLocalSession(m.agent), nil
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

// statusLine is the top bar's content: active agent, session id, and
// mode badges -- or the last error, if there is one, in the same slot
// (rather than an extra line) so the tab's height budget never varies
// with whether an error happens to be showing. Always truncated to
// m.width -- see SetSize's doc comment for why that matters here.
func (m chatTabModel) statusLine() string {
	if m.err != nil {
		return truncateToWidth(errStyle.Render("error: "+m.err.Error()), m.width)
	}

	status := fmt.Sprintf("%s (%s)", m.agent, m.sessionID)
	if !m.sessionCreated {
		status += " " + dimStyle.Render("[not saved yet]")
	}
	if m.autoMode {
		status += " " + successStyle.Render("[auto-mode]")
	}
	return truncateToWidth(dimStyle.Render(status), m.width)
}

// inputBoxContent is the bottom box's content: the live input, a
// thinking spinner, or a pending confirmation's y/n prompt. hint text
// comes from the server (a tool's confirmation message) and isn't
// length-bounded, so it's truncated the same way statusLine is.
func (m chatTabModel) inputBoxContent() string {
	switch {
	case m.pending != nil:
		return truncateToWidth(promptStyle.Render(fmt.Sprintf("%s [y/n] ", m.pending.hint)), m.width)
	case m.waiting:
		return m.spinner.View() + " thinking..."
	default:
		return m.input.View()
	}
}

func (m chatTabModel) View() string {
	if !m.ready {
		return "initializing...\n"
	}

	statusBar := chatStatusBarStyle.Width(m.width).Render(m.statusLine())
	inputBox := chatInputBoxStyle.Width(m.width).Render(m.inputBoxContent())

	return fmt.Sprintf("%s\n%s\n%s", statusBar, m.viewport.View(), inputBox)
}
