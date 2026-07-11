package chat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionsTabModel is the F2 tab: a browsable table of every session
// across every agent/user, backed by GET /botson/sessions.
type sessionsTabModel struct {
	ctx    context.Context
	client *client

	table    table.Model
	sessions []sessionStat

	// autoMode tracks which rows are known to be in auto-mode, keyed by
	// compositeKey. GET /botson/sessions doesn't return the flag (it's
	// session state, not a summary field -- see management.SessionStat),
	// so this only reflects toggles made from this tab this run, not a
	// fetched ground truth; the "Auto" column is blank until then.
	autoMode map[string]bool

	loaded    bool
	loading   bool
	armDelete int // index into m.sessions awaiting a second 'd' to confirm, or -1
	err       error
	status    string
}

func newSessionsTab(ctx context.Context, c *client) sessionsTabModel {
	columns := []table.Column{
		{Title: "Agent", Width: 20},
		{Title: "User", Width: 16},
		{Title: "Name", Width: 22},
		{Title: "Updated", Width: 16},
		{Title: "Events", Width: 7},
		{Title: "Auto", Width: 5},
	}
	return sessionsTabModel{
		ctx:       ctx,
		client:    c,
		table:     table.New(table.WithColumns(columns), table.WithFocused(true)),
		autoMode:  map[string]bool{},
		armDelete: -1,
	}
}

func (m sessionsTabModel) Init() tea.Cmd { return nil }

func (m *sessionsTabModel) SetSize(width, height int) {
	const statusHeight = 1
	m.table.SetWidth(width)
	m.table.SetHeight(height - statusHeight)
}

func compositeKey(agent, user, sessionID string) string {
	return agent + "/" + user + "/" + sessionID
}

// sessionsLoadedMsg carries the result of a ListSessions call.
type sessionsLoadedMsg struct {
	sessions []sessionStat
	err      error
}

// sessionDeletedMsg carries the result of a DeleteSession call.
type sessionDeletedMsg struct {
	key string
	err error
}

// sessionRowAutoModeToggledMsg carries the result of a SetSessionAutoMode
// call triggered from this tab -- distinct from chatTabModel's own
// autoModeSetMsg, which is scoped to whatever session Chat currently has
// open.
type sessionRowAutoModeToggledMsg struct {
	key     string
	enabled bool
	err     error
}

func (m sessionsTabModel) load() tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		sessions, err := c.ListSessions(ctx, "", "")
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

func (m sessionsTabModel) rows() []table.Row {
	rows := make([]table.Row, len(m.sessions))
	for i, s := range m.sessions {
		name := s.DisplayName
		if name == "" {
			name = s.ID
		}
		auto := ""
		if on, known := m.autoMode[compositeKey(s.AgentName, s.UserID, s.ID)]; known && on {
			auto = "on"
		} else if known {
			auto = "off"
		}
		rows[i] = table.Row{
			s.AgentName,
			s.UserID,
			name,
			time.Unix(s.LastUpdateTime, 0).Local().Format("2006-01-02 15:04"),
			strconv.Itoa(s.EventCount),
			auto,
		}
	}
	return rows
}

func (m sessionsTabModel) Update(msg tea.Msg) (sessionsTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		m.loading, m.loaded = false, true
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.sessions = msg.sessions
		m.table.SetRows(m.rows())
		return m, nil

	case sessionDeletedMsg:
		m.status = ""
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		delete(m.autoMode, msg.key)
		return m, m.load()

	case sessionRowAutoModeToggledMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.autoMode[msg.key] = msg.enabled
		m.table.SetRows(m.rows())
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m sessionsTabModel) handleKey(msg tea.KeyMsg) (sessionsTabModel, tea.Cmd) {
	key := msg.String()
	if key != "d" && m.armDelete != -1 {
		m.armDelete = -1
		m.status = ""
	}

	switch key {
	case "r":
		m.loading = true
		m.err = nil
		return m, m.load()

	case "enter":
		s, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, func() tea.Msg {
			return switchSessionMsg{agent: s.AgentName, user: s.UserID, sessionID: s.ID}
		}

	case "a":
		s, ok := m.selected()
		if !ok {
			return m, nil
		}
		ck := compositeKey(s.AgentName, s.UserID, s.ID)
		enabled := !m.autoMode[ck]
		c, ctx := m.client, m.ctx
		return m, func() tea.Msg {
			err := c.SetSessionAutoMode(ctx, s.AgentName, s.UserID, s.ID, enabled)
			return sessionRowAutoModeToggledMsg{key: ck, enabled: enabled, err: err}
		}

	case "d":
		s, ok := m.selected()
		if !ok {
			return m, nil
		}
		idx := m.table.Cursor()
		if m.armDelete == idx {
			m.armDelete = -1
			m.status = ""
			c, ctx := m.client, m.ctx
			ck := compositeKey(s.AgentName, s.UserID, s.ID)
			return m, func() tea.Msg {
				err := c.DeleteSession(ctx, s.AgentName, s.UserID, s.ID)
				return sessionDeletedMsg{key: ck, err: err}
			}
		}
		m.armDelete = idx
		m.status = fmt.Sprintf("delete %q -- press d again to confirm", s.displayName())
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m sessionsTabModel) selected() (sessionStat, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.sessions) {
		return sessionStat{}, false
	}
	return m.sessions[idx], true
}

func (s sessionStat) displayName() string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	return s.ID
}

func (m sessionsTabModel) View() string {
	var status string
	switch {
	case m.err != nil:
		status = errStyle.Render("error: " + m.err.Error())
	case m.status != "":
		status = promptStyle.Render(m.status)
	case m.loading:
		status = dimStyle.Render("loading...")
	case m.loaded && len(m.sessions) == 0:
		status = dimStyle.Render("no sessions yet")
	default:
		status = dimStyle.Render("enter: open in chat  a: toggle auto-mode  d: delete  r: refresh")
	}
	return fmt.Sprintf("%s\n%s", m.table.View(), status)
}
