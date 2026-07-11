package chat

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func readyRootModel(t *testing.T) model {
	t.Helper()
	m := newModel(context.Background(), newClient("http://example.invalid", "tok"), "Agent Botson", "chat-alice", "sess-1")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", updated)
	}
	return mm
}

func TestModel_CtrlCQuits(t *testing.T) {
	m := readyRootModel(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a non-nil cmd for ctrl+c")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestModel_FKeysSwitchTabs(t *testing.T) {
	tests := []struct {
		key  tea.KeyType
		want tabID
	}{
		{tea.KeyF1, tabChat},
		{tea.KeyF2, tabSessions},
		{tea.KeyF3, tabAgents},
		{tea.KeyF4, tabSettings},
		{tea.KeyF5, tabStats},
	}
	for _, tt := range tests {
		m := readyRootModel(t)
		updated, _ := m.Update(tea.KeyMsg{Type: tt.key})
		mm := updated.(model)
		if mm.activeTab != tt.want {
			t.Errorf("F-key %v: activeTab = %v, want %v", tt.key, mm.activeTab, tt.want)
		}
	}
}

func TestModel_FirstVisitLazyLoadsTabData(t *testing.T) {
	m := readyRootModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	mm := updated.(model)

	if !mm.sessionsTab.loading {
		t.Error("expected sessions tab to start loading on first visit")
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to fetch sessions on first visit")
	}

	// A second visit shouldn't re-trigger the load.
	mm.activeTab = tabChat
	updated2, cmd2 := mm.Update(tea.KeyMsg{Type: tea.KeyF2})
	mm2 := updated2.(model)
	if mm2.activeTab != tabSessions {
		t.Fatalf("activeTab = %v, want tabSessions", mm2.activeTab)
	}
	if cmd2 != nil {
		t.Error("expected no reload cmd on a revisit")
	}
}

func TestModel_SwitchSessionMsgRoutesToChatAndSwitchesTab(t *testing.T) {
	m := readyRootModel(t)
	m.activeTab = tabSessions

	updated, cmd := m.Update(switchSessionMsg{agent: "Agent Botson", user: "chat-alice", sessionID: "sess-2"})
	mm := updated.(model)

	if mm.activeTab != tabChat {
		t.Errorf("activeTab = %v, want tabChat", mm.activeTab)
	}
	if !mm.chatTab.waiting {
		t.Error("expected chat tab to start loading the switched-to session")
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to fetch the session")
	}
}

func TestModel_SwitchAgentMsgRoutesToChatAndSwitchesTab(t *testing.T) {
	m := readyRootModel(t)
	m.activeTab = tabAgents

	updated, cmd := m.Update(switchAgentMsg{agent: "Other Agent"})
	mm := updated.(model)

	if mm.activeTab != tabChat {
		t.Errorf("activeTab = %v, want tabChat", mm.activeTab)
	}
	if !mm.chatTab.waiting {
		t.Error("expected chat tab to start creating a session for the new agent")
	}
	if cmd == nil {
		t.Error("expected a non-nil cmd to create the session")
	}
}

func TestModel_TurnResultRoutesToChatTabRegardlessOfActiveTab(t *testing.T) {
	m := readyRootModel(t)
	m.activeTab = tabStats // user is looking at another tab
	m.chatTab.waiting = true

	updated, _ := m.Update(turnResultMsg{})
	mm := updated.(model)

	if mm.activeTab != tabStats {
		t.Errorf("activeTab changed to %v, want it to stay tabStats", mm.activeTab)
	}
	if mm.chatTab.waiting {
		t.Error("expected the chat tab's turn result to still be processed in the background")
	}
}
