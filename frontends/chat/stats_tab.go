package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// statsTabModel is the F5 tab: a read-only glance view of
// GET /botson/dashboard/stats -- stat tiles, a per-agent session-count
// list, and an interactive recent-sessions table (enter opens a session
// in Chat, consistent with the Sessions tab).
type statsTabModel struct {
	ctx    context.Context
	client *client

	stats *dashboardStats
	table table.Model

	loaded  bool
	loading bool
	err     error
}

func newStatsTab(ctx context.Context, c *client) statsTabModel {
	columns := []table.Column{
		{Title: "Agent", Width: 20},
		{Title: "User", Width: 16},
		{Title: "Name", Width: 22},
		{Title: "Updated", Width: 16},
	}
	return statsTabModel{
		ctx:    ctx,
		client: c,
		table:  table.New(table.WithColumns(columns), table.WithFocused(true)),
	}
}

func (m statsTabModel) Init() tea.Cmd { return nil }

// SetSize gives the recent-sessions table most of the vertical space,
// reserving a fixed budget above it for the stat tiles and per-agent
// list -- approximate (the per-agent list can grow with agent count) but
// good enough for a glance view; it just scrolls the table region if the
// budget runs tight.
func (m *statsTabModel) SetSize(width, height int) {
	const reservedForTilesAndAgents = 11
	m.table.SetWidth(width)
	m.table.SetHeight(max(height-reservedForTilesAndAgents, 3))
}

// statsLoadedMsg carries the result of a GetDashboardStats call.
type statsLoadedMsg struct {
	stats *dashboardStats
	err   error
}

func (m statsTabModel) load() tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		stats, err := c.GetDashboardStats(ctx)
		return statsLoadedMsg{stats: stats, err: err}
	}
}

func recentSessionRows(sessions []sessionStat) []table.Row {
	rows := make([]table.Row, len(sessions))
	for i, s := range sessions {
		rows[i] = table.Row{
			s.AgentName,
			s.UserID,
			s.displayName(),
			time.Unix(s.LastUpdateTime, 0).Local().Format("2006-01-02 15:04"),
		}
	}
	return rows
}

func (m statsTabModel) Update(msg tea.Msg) (statsTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case statsLoadedMsg:
		m.loading, m.loaded = false, true
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.stats = msg.stats
		m.table.SetRows(recentSessionRows(msg.stats.RecentSessions))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m statsTabModel) handleKey(msg tea.KeyMsg) (statsTabModel, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.loading = true
		m.err = nil
		return m, m.load()

	case "enter":
		if m.stats == nil {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.stats.RecentSessions) {
			return m, nil
		}
		s := m.stats.RecentSessions[idx]
		return m, func() tea.Msg {
			return switchSessionMsg{agent: s.AgentName, user: s.UserID, sessionID: s.ID}
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m statsTabModel) View() string {
	if m.err != nil {
		return errStyle.Render("error: " + m.err.Error())
	}
	if m.loading || m.stats == nil {
		return dimStyle.Render("loading...")
	}

	tiles := lipgloss.JoinHorizontal(lipgloss.Top,
		statTile("Agents", strconv.Itoa(m.stats.TotalAgents)),
		statTile("Sessions", strconv.Itoa(m.stats.TotalSessions)),
		statTile("Events", strconv.Itoa(m.stats.TotalEvents)),
	)

	var agentsBlock strings.Builder
	agentsBlock.WriteString(dimStyle.Render("Per-agent sessions:") + "\n")
	for _, a := range m.stats.Agents {
		root := ""
		if a.IsRoot {
			root = " " + badgeStyle.Render("root")
		}
		fmt.Fprintf(&agentsBlock, "  %s%s: %d\n", a.Name, root, a.SessionCount)
	}

	dbLine := dimStyle.Render("DB: " + m.stats.DbPath)

	return fmt.Sprintf("%s\n\n%s\n%s\n\n%s\n%s",
		tiles,
		agentsBlock.String(),
		dbLine,
		dimStyle.Render("Recent sessions (enter: open in chat  r: refresh):"),
		m.table.View(),
	)
}

func statTile(label, value string) string {
	return statTileStyle.Render(statValueStyle.Render(value) + "\n" + statLabelStyle.Render(label))
}
