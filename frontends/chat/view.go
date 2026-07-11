package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

func (m model) View() string {
	if m.width == 0 {
		return "initializing...\n"
	}

	var body string
	switch m.activeTab {
	case tabChat:
		body = m.chatTab.View()
	case tabSessions:
		body = m.sessionsTab.View()
	case tabAgents:
		body = m.agentsTab.View()
	case tabSettings:
		body = m.settingsTab.View()
	case tabStats:
		body = m.statsTab.View()
	}

	return m.renderTabBar() + "\n" + body + "\n" + m.help.View(m.activeHelpKeyMap())
}

// renderTabBar shows each tab's own F-key hint directly on its label
// (e.g. "F1 Chat") instead of leaving that mapping only in the footer --
// relies on tabID's declaration order in model.go lining up with
// globalKeys' F1-F5 bindings (tabChat=0 -> F1, tabSessions=1 -> F2, ...),
// the same order globalKeys itself is defined in.
func (m model) renderTabBar() string {
	var b strings.Builder
	for t := tabID(0); t < tabID(len(tabNames)); t++ {
		style := inactiveTabStyle
		if t == m.activeTab {
			style = activeTabStyle
		}
		label := fmt.Sprintf("F%d %s", t+1, tabNames[t])
		b.WriteString(style.Render(label))
	}
	return tabBarStyle.Width(m.width).Render(b.String())
}

// activeHelpKeyMap concatenates the always-present global bindings with
// whichever tab is currently active, so the footer shows exactly the
// keys that do something right now.
func (m model) activeHelpKeyMap() help.KeyMap {
	var tabHelp []key.Binding
	switch m.activeTab {
	case tabChat:
		tabHelp = chatKeys.ShortHelp()
	case tabSessions:
		tabHelp = sessionsKeys.ShortHelp()
	case tabAgents:
		tabHelp = agentsKeys.ShortHelp()
	case tabSettings:
		tabHelp = settingsKeys.ShortHelp()
	case tabStats:
		tabHelp = statsKeys.ShortHelp()
	}
	return keyMapList(append(globalKeys.ShortHelp(), tabHelp...))
}
