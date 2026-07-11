package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"botson/internal/networking/adkwire"
)

func readyChatTab(t *testing.T) chatTabModel {
	t.Helper()
	m := newChatTab(context.Background(), newClient("http://example.invalid", "tok"), "Agent Botson", "chat-alice", "sess-1")
	m.SetSize(80, 24)
	if !m.ready {
		t.Fatal("chat tab did not become ready after SetSize")
	}
	return m
}

func TestChatTab_EnterSubmitsMessageAndSetsWaiting(t *testing.T) {
	m := readyChatTab(t)
	m.input.SetValue("hello")

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !mm.waiting {
		t.Error("expected waiting=true after submitting a message")
	}
	if cmd == nil {
		t.Error("expected a non-nil tea.Cmd to run the turn")
	}
	if mm.input.Value() != "" {
		t.Errorf("expected input to be cleared, got %q", mm.input.Value())
	}
	if len(mm.history) != 1 || mm.history[0] != "you: hello" {
		t.Errorf("history = %v, want [\"you: hello\"]", mm.history)
	}
}

func TestChatTab_EnterWithEmptyInputDoesNothing(t *testing.T) {
	m := readyChatTab(t)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if mm.waiting {
		t.Error("expected waiting to remain false for an empty submission")
	}
	if cmd != nil {
		t.Error("expected no cmd for an empty submission")
	}
}

func TestChatTab_WaitingSwallowsInput(t *testing.T) {
	m := readyChatTab(t)
	m.waiting = true

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if mm.input.Value() != "" {
		t.Errorf("expected input to stay empty while waiting, got %q", mm.input.Value())
	}
	if cmd != nil {
		t.Error("expected no cmd while waiting swallows a keystroke")
	}
}

func TestChatTab_PendingConfirmation_YConfirms(t *testing.T) {
	m := readyChatTab(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if mm.pending != nil {
		t.Error("expected pending to be cleared after y")
	}
	if !mm.waiting {
		t.Error("expected waiting=true after answering a confirmation")
	}
	if cmd == nil {
		t.Error("expected a non-nil tea.Cmd to send the confirmation answer")
	}
}

func TestChatTab_PendingConfirmation_NRejects(t *testing.T) {
	m := readyChatTab(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	if mm.pending != nil {
		t.Error("expected pending to be cleared after n")
	}
	if !mm.waiting {
		t.Error("expected waiting=true after answering a confirmation")
	}
	if cmd == nil {
		t.Error("expected a non-nil tea.Cmd to send the confirmation answer")
	}
}

func TestChatTab_PendingConfirmation_OtherKeysIgnored(t *testing.T) {
	m := readyChatTab(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if mm.pending == nil {
		t.Error("expected pending to remain set for a non-y/n key")
	}
	if mm.waiting {
		t.Error("expected waiting to remain false for a non-y/n key")
	}
	if cmd != nil {
		t.Error("expected no cmd for a non-y/n key while a confirmation is pending")
	}
}

func TestChatTab_TurnResultRendersAndClearsWaiting(t *testing.T) {
	m := readyChatTab(t)
	m.waiting = true

	mm, _ := m.Update(turnResultMsg{events: []adkwire.Event{
		{Author: "Agent Botson", Content: nil},
	}})

	if mm.waiting {
		t.Error("expected waiting=false after a turn result")
	}
}

func TestChatTab_TurnResultError(t *testing.T) {
	m := readyChatTab(t)
	m.waiting = true

	mm, _ := m.Update(turnResultMsg{err: context.DeadlineExceeded})

	if mm.waiting {
		t.Error("expected waiting=false even on error")
	}
	if mm.err == nil {
		t.Error("expected err to be set")
	}
}

func TestChatTab_SwitchAgentMsgResetsSessionLocallyWithNoNetworkCall(t *testing.T) {
	m := readyChatTab(t)
	m.waiting = true
	m.pending = &pendingConfirmation{callID: "c1", hint: "x"}
	m.history = []string{"you: hi"}
	m.sessionID = "sess-old"
	m.sessionCreated = true

	mm, cmd := m.Update(switchAgentMsg{agent: "Other Agent"})

	if cmd != nil {
		t.Error("expected no cmd -- switching agent is a purely local reset, no network call until a message is sent")
	}
	if mm.waiting {
		t.Error("expected waiting=false, since nothing async is happening")
	}
	if mm.pending != nil {
		t.Error("expected pending to be cleared")
	}
	if mm.agent != "Other Agent" {
		t.Errorf("agent = %q, want Other Agent", mm.agent)
	}
	if mm.sessionID == "sess-old" {
		t.Error("expected a freshly generated sessionID")
	}
	if mm.sessionCreated {
		t.Error("expected sessionCreated=false for the fresh, not-yet-used session")
	}
	if len(mm.history) != 0 {
		t.Errorf("expected history to be reset, got %v", mm.history)
	}
}

func TestChatTab_CtrlNResetsSessionLocallyWithNoNetworkCall(t *testing.T) {
	m := readyChatTab(t)
	m.sessionID = "sess-old"
	m.sessionCreated = true
	m.history = []string{"you: hi"}

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})

	if cmd != nil {
		t.Error("expected no cmd -- ctrl+n is a purely local reset, no network call until a message is sent")
	}
	if mm.sessionID == "sess-old" {
		t.Error("expected a freshly generated sessionID")
	}
	if mm.sessionCreated {
		t.Error("expected sessionCreated=false for the fresh, not-yet-used session")
	}
	if len(mm.history) != 0 {
		t.Errorf("expected history to be reset, got %v", mm.history)
	}
}

func TestChatTab_TurnResultMarksSessionCreated(t *testing.T) {
	m := readyChatTab(t)
	if m.sessionCreated {
		t.Fatal("expected a freshly constructed chat tab to start with sessionCreated=false")
	}

	mm, _ := m.Update(turnResultMsg{})

	if !mm.sessionCreated {
		t.Error("expected sessionCreated=true after any turn succeeds, whether or not this tab created it")
	}
}

func TestChatTab_EnsureSessionAndRunTurn_CreatesSessionOnceThenReuses(t *testing.T) {
	var createCalls, runCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/sessions/"):
			createCalls++
		case r.URL.Path == "/api/run":
			runCalls++
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/sessions/") {
			_, _ = w.Write([]byte(`{}`))
		} else {
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	m := newChatTab(context.Background(), newClient(srv.URL, "tok"), "Agent Botson", "chat-alice", "sess-1")
	m.SetSize(80, 24)

	// First message: session doesn't exist yet, must be created first.
	cmd := m.submitMessage("hello")
	msg := cmd()
	if _, ok := msg.(turnResultMsg); !ok {
		t.Fatalf("expected a turnResultMsg, got %T", msg)
	}
	if createCalls != 1 {
		t.Errorf("createCalls = %d, want 1 (session should be created lazily on first use)", createCalls)
	}
	if runCalls != 1 {
		t.Errorf("runCalls = %d, want 1", runCalls)
	}

	m, _ = m.Update(msg) // mark sessionCreated via the returned turnResultMsg

	// Second message: session already exists, must not be re-created.
	cmd = m.submitMessage("again")
	cmd()
	if createCalls != 1 {
		t.Errorf("createCalls = %d, want still 1 (must not recreate an already-created session)", createCalls)
	}
	if runCalls != 2 {
		t.Errorf("runCalls = %d, want 2", runCalls)
	}
}

func TestChatTab_SessionLoadedMsgReplaysHistoryAndAutoMode(t *testing.T) {
	m := readyChatTab(t)
	m.waiting = true

	mm, _ := m.Update(sessionLoadedMsg{
		agent: "Agent Botson", user: "chat-bob", sessionID: "sess-9",
		detail: &sessionDetail{
			State: map[string]any{"botson:autoMode": true},
			Events: []sessionEventSummary{
				{Author: "chat-bob", Text: "hi"},
				{Author: "Agent Botson", Text: "hello"},
				{Author: "system", Text: ""}, // empty text is skipped
			},
		},
	})

	if mm.waiting {
		t.Error("expected waiting=false after sessionLoadedMsg")
	}
	if mm.agent != "Agent Botson" || mm.user != "chat-bob" || mm.sessionID != "sess-9" {
		t.Errorf("identity = %+v, want Agent Botson/chat-bob/sess-9", mm)
	}
	if !mm.autoMode {
		t.Error("expected autoMode=true from session state")
	}
	want := []string{"chat-bob: hi", "Agent Botson: hello"}
	if len(mm.history) != len(want) || mm.history[0] != want[0] || mm.history[1] != want[1] {
		t.Errorf("history = %v, want %v", mm.history, want)
	}
}

func TestChatTab_SwitchSessionMsgTriggersLoad(t *testing.T) {
	m := readyChatTab(t)

	mm, cmd := m.Update(switchSessionMsg{agent: "Agent Botson", user: "chat-alice", sessionID: "sess-3"})

	if !mm.waiting {
		t.Error("expected waiting=true while the session loads")
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to fetch the session")
	}
}

func TestChatTab_CtrlAToggleAutoModeSetsWaiting(t *testing.T) {
	m := readyChatTab(t)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})

	if !mm.waiting {
		t.Error("expected waiting=true while the toggle is in flight")
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to send the toggle")
	}
}
