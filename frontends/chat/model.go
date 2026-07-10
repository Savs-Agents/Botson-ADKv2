package chat

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

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

// model is the Bubble Tea model for the chat TUI.
type model struct {
	ctx    context.Context
	client *client

	agent     string
	user      string
	sessionID string

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	ready   bool // WindowSizeMsg has arrived, viewport/input are sized
	waiting bool // a turn is in flight
	pending *pendingConfirmation
	err     error

	history []string // rendered lines, newest last
}

func newModel(ctx context.Context, c *client, agent, user, sessionID string) model {
	ti := textinput.New()
	ti.Placeholder = "message " + agent + "..."
	ti.Focus()
	ti.CharLimit = 4000

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		ctx:       ctx,
		client:    c,
		agent:     agent,
		user:      user,
		sessionID: sessionID,
		input:     ti,
		spinner:   sp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

// turnResultMsg is delivered when a RunTurn call (from either a submitted
// user message or a HITL confirmation answer) completes.
type turnResultMsg struct {
	events []adkwire.Event
	err    error
}

// submitMessage sends text as a new user turn.
func (m model) submitMessage(text string) tea.Cmd {
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
func (m model) answerConfirmation(confirmed bool) tea.Cmd {
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

func (m model) runTurn(req adkwire.RunAgentRequest) tea.Cmd {
	return func() tea.Msg {
		events, err := m.client.RunTurn(m.ctx, req)
		return turnResultMsg{events: events, err: err}
	}
}
