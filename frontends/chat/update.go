package chat

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg), nil

	case tea.KeyMsg:
		if cmd, handled := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		return m.delegateToActiveTab(msg)

	case switchSessionMsg:
		var cmd tea.Cmd
		m.chatTab, cmd = m.chatTab.Update(msg)
		m.activeTab = tabChat
		m.visited[tabChat] = true
		return m, cmd

	case switchAgentMsg:
		var cmd tea.Cmd
		m.chatTab, cmd = m.chatTab.Update(msg)
		m.activeTab = tabChat
		m.visited[tabChat] = true
		return m, cmd

	// Every case below is an async result owned by exactly one tab.
	// These are always routed to that tab regardless of which tab is
	// currently active -- e.g. a chat turn in flight keeps progressing
	// (and its spinner keeps animating) while the user is looking at the
	// Sessions tab, the same way a real dashboard's panels keep working
	// in the background.
	case turnResultMsg, sessionLoadedMsg, autoModeSetMsg, spinner.TickMsg:
		var cmd tea.Cmd
		m.chatTab, cmd = m.chatTab.Update(msg)
		return m, cmd

	case sessionsLoadedMsg, sessionDeletedMsg, sessionRowAutoModeToggledMsg:
		var cmd tea.Cmd
		m.sessionsTab, cmd = m.sessionsTab.Update(msg)
		return m, cmd

	case agentsLoadedMsg, agentDeletedMsg:
		var cmd tea.Cmd
		m.agentsTab, cmd = m.agentsTab.Update(msg)
		return m, cmd

	case settingsLoadedMsg, settingsSavedMsg:
		var cmd tea.Cmd
		m.settingsTab, cmd = m.settingsTab.Update(msg)
		return m, cmd

	case statsLoadedMsg:
		var cmd tea.Cmd
		m.statsTab, cmd = m.statsTab.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleGlobalKey processes keys the root intercepts before a tab ever
// sees them: quit and the F1-F5 panel jumps. F-keys, not digits -- see
// globalKeyMap's doc comment for why plain digits can't be used here.
func (m *model) handleGlobalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, globalKeys.Quit):
		return tea.Quit, true
	case key.Matches(msg, globalKeys.TabChat):
		return m.switchTab(tabChat), true
	case key.Matches(msg, globalKeys.TabSessions):
		return m.switchTab(tabSessions), true
	case key.Matches(msg, globalKeys.TabAgents):
		return m.switchTab(tabAgents), true
	case key.Matches(msg, globalKeys.TabSettings):
		return m.switchTab(tabSettings), true
	case key.Matches(msg, globalKeys.TabStats):
		return m.switchTab(tabStats), true
	}
	return nil, false
}

// switchTab makes t the active tab and, the first time t is visited,
// kicks off its data load.
func (m *model) switchTab(t tabID) tea.Cmd {
	m.activeTab = t
	if m.visited[t] {
		return nil
	}
	m.visited[t] = true

	switch t {
	case tabSessions:
		m.sessionsTab.loading = true
		return m.sessionsTab.load()
	case tabAgents:
		m.agentsTab.loading = true
		return m.agentsTab.load()
	case tabSettings:
		m.settingsTab.loading = true
		return m.settingsTab.load()
	case tabStats:
		m.statsTab.loading = true
		return m.statsTab.load()
	}
	return nil
}

func (m model) delegateToActiveTab(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activeTab {
	case tabChat:
		m.chatTab, cmd = m.chatTab.Update(msg)
	case tabSessions:
		m.sessionsTab, cmd = m.sessionsTab.Update(msg)
	case tabAgents:
		m.agentsTab, cmd = m.agentsTab.Update(msg)
	case tabSettings:
		m.settingsTab, cmd = m.settingsTab.Update(msg)
	case tabStats:
		m.statsTab, cmd = m.statsTab.Update(msg)
	}
	return m, cmd
}

// handleResize lays out every tab for the new terminal size -- all five,
// not just the active one, so a tab is already correctly sized the
// moment the user switches to it instead of needing its own resize.
func (m model) handleResize(msg tea.WindowSizeMsg) model {
	m.width, m.height = msg.Width, msg.Height

	const tabBarHeight, footerHeight = 2, 1 // tabBarStyle's label line + its bottom border, plus the help footer
	contentHeight := max(msg.Height-tabBarHeight-footerHeight, 0)

	m.chatTab.SetSize(msg.Width, contentHeight)
	m.sessionsTab.SetSize(msg.Width, contentHeight)
	m.agentsTab.SetSize(msg.Width, contentHeight)
	m.settingsTab.SetSize(msg.Width, contentHeight)
	m.statsTab.SetSize(msg.Width, contentHeight)
	m.help.Width = msg.Width

	return m
}
