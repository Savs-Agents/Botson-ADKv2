package chat

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"botson/internal/networking/adkwire"
)

func readyModel(t *testing.T) model {
	t.Helper()
	m := newModel(context.Background(), newClient("http://example.invalid", "tok"), "Agent Botson", "chat:alice", "sess-1")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := updated.(model)
	if !mm.ready {
		t.Fatal("model did not become ready after a WindowSizeMsg")
	}
	return mm
}

func TestUpdate_EnterSubmitsMessageAndSetsWaiting(t *testing.T) {
	m := readyModel(t)
	m.input.SetValue("hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := updated.(model)

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

func TestUpdate_EnterWithEmptyInputDoesNothing(t *testing.T) {
	m := readyModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := updated.(model)

	if mm.waiting {
		t.Error("expected waiting to remain false for an empty submission")
	}
	if cmd != nil {
		t.Error("expected no cmd for an empty submission")
	}
}

func TestUpdate_WaitingSwallowsInput(t *testing.T) {
	m := readyModel(t)
	m.waiting = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	mm := updated.(model)

	if mm.input.Value() != "" {
		t.Errorf("expected input to stay empty while waiting, got %q", mm.input.Value())
	}
	if cmd != nil {
		t.Error("expected no cmd while waiting swallows a keystroke")
	}
}

func TestUpdate_PendingConfirmation_YConfirms(t *testing.T) {
	m := readyModel(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	mm := updated.(model)

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

func TestUpdate_PendingConfirmation_NRejects(t *testing.T) {
	m := readyModel(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	mm := updated.(model)

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

func TestUpdate_PendingConfirmation_OtherKeysIgnored(t *testing.T) {
	m := readyModel(t)
	m.pending = &pendingConfirmation{callID: "c1", hint: "Approve?"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := updated.(model)

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

func TestUpdate_TurnResultRendersAndClearsWaiting(t *testing.T) {
	m := readyModel(t)
	m.waiting = true

	updated, _ := m.Update(turnResultMsg{events: []adkwire.Event{
		{Author: "Agent Botson", Content: nil},
	}})
	mm := updated.(model)

	if mm.waiting {
		t.Error("expected waiting=false after a turn result")
	}
}

func TestUpdate_TurnResultError(t *testing.T) {
	m := readyModel(t)
	m.waiting = true

	updated, _ := m.Update(turnResultMsg{err: context.DeadlineExceeded})
	mm := updated.(model)

	if mm.waiting {
		t.Error("expected waiting=false even on error")
	}
	if mm.err == nil {
		t.Error("expected err to be set")
	}
}

func TestUpdate_CtrlCQuits(t *testing.T) {
	m := readyModel(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a non-nil cmd for ctrl+c")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}
