package chat

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

// tabID identifies one of the dashboard's five panels.
type tabID int

const (
	tabChat tabID = iota
	tabSessions
	tabAgents
	tabSettings
	tabStats
)

var tabNames = [...]string{
	tabChat:     "Chat",
	tabSessions: "Sessions",
	tabAgents:   "Agents",
	tabSettings: "Settings",
	tabStats:    "Stats",
}

// switchSessionMsg asks the Chat tab to load and switch to the given
// session, and switches the root's active tab to Chat -- emitted by the
// Sessions and Stats tabs on "enter".
type switchSessionMsg struct {
	agent, user, sessionID string
}

// switchAgentMsg asks the Chat tab to start a fresh session against the
// given agent, and switches the root's active tab to Chat -- emitted by
// the Agents tab on "enter".
type switchAgentMsg struct {
	agent string
}

// model is the root Bubble Tea model: a tab bar over five composed
// sub-models (chat_tab.go/sessions_tab.go/agents_tab.go/settings_tab.go/
// stats_tab.go), each with its own Init/Update/View. Chat is the
// default/first tab; the other four lazy-load their data the first time
// they're switched to (see update.go's switchTab), not eagerly at
// startup, so opening the dashboard doesn't fire four unused requests.
type model struct {
	width, height int
	activeTab     tabID
	visited       [len(tabNames)]bool

	chatTab     chatTabModel
	sessionsTab sessionsTabModel
	agentsTab   agentsTabModel
	settingsTab settingsTabModel
	statsTab    statsTabModel

	help help.Model
}

func newModel(ctx context.Context, c *client, agent, user, sessionID string) model {
	m := model{
		chatTab:     newChatTab(ctx, c, agent, user, sessionID),
		sessionsTab: newSessionsTab(ctx, c),
		agentsTab:   newAgentsTab(ctx, c),
		settingsTab: newSettingsTab(ctx, c),
		statsTab:    newStatsTab(ctx, c),
		help:        help.New(),
	}
	m.visited[tabChat] = true
	return m
}

func (m model) Init() tea.Cmd {
	return m.chatTab.Init()
}
