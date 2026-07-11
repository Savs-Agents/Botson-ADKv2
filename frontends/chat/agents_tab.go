package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// agentItem adapts agentDetail to bubbles/list's Item/DefaultItem
// interfaces. Its own Description() method shadows the embedded
// agentDetail.Description field (Go: a directly-defined method wins over
// a promoted field of the same name) -- intentional, since DefaultItem
// requires Description() as a method; the field is still reachable
// explicitly via a.agentDetail.Description.
type agentItem struct {
	agentDetail
}

func (a agentItem) FilterValue() string { return a.Name }

func (a agentItem) Title() string {
	title := a.Name
	if a.IsRoot {
		title += " " + badgeStyle.Render("root")
	}
	if a.ReadOnly {
		title += " " + badgeStyle.Render("read-only")
	}
	if a.Private {
		title += " " + badgeStyle.Render("private")
	}
	return title
}

func (a agentItem) Description() string { return a.agentDetail.Description }

// agentsTabModel is the F3 tab: a master/detail browser over
// GET /botson/agents -- the list on the left, the selected agent's
// instructions/tools on the right.
type agentsTabModel struct {
	ctx    context.Context
	client *client

	list   list.Model
	detail viewport.Model
	agents []agentDetail

	ready bool

	loaded    bool
	loading   bool
	armDelete string // name of an agent awaiting a second 'x' to confirm, or ""
	err       error
	status    string
}

func newAgentsTab(ctx context.Context, c *client) agentsTabModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Agents"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return agentsTabModel{ctx: ctx, client: c, list: l}
}

func (m agentsTabModel) Init() tea.Cmd { return nil }

// SetSize splits width into a ~1/3 list pane and a bordered detail pane
// for the remainder, accounting for paneBorderStyle's own border+padding
// overhead so the two panes plus gutter sum to exactly width.
func (m *agentsTabModel) SetSize(width, height int) {
	const statusHeight = 1 // View()'s trailing status line -- must be reserved here or the two panes overflow the height budget by exactly one line
	paneHeight := max(height-statusHeight, 0)

	listWidth := width / 3
	if listWidth < 24 {
		listWidth = 24
	}
	if listWidth > width-10 {
		listWidth = max(width-10, 0)
	}
	m.list.SetSize(listWidth, paneHeight)

	const paneOverhead = 4 // paneBorderStyle's border (2 cols) + padding (2 cols)
	detailWidth := max(width-listWidth-paneOverhead, 0)
	detailHeight := max(paneHeight-2, 0) // paneBorderStyle's top/bottom border

	if !m.ready {
		m.detail = viewport.New(detailWidth, detailHeight)
		m.ready = true
	} else {
		m.detail.Width = detailWidth
		m.detail.Height = detailHeight
	}
	m.syncDetail()
}

// syncDetail rebuilds the detail pane's content from whichever agent is
// currently selected in the list.
func (m *agentsTabModel) syncDetail() {
	item, ok := m.list.SelectedItem().(agentItem)
	if !ok {
		m.detail.SetContent(dimStyle.Render("no agent selected"))
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", accentStyle.Render(item.Name))
	if item.Description() != "" {
		fmt.Fprintf(&b, "%s\n\n", item.Description())
	}
	b.WriteString(dimStyle.Render("Tools:") + "\n")
	if len(item.Tools) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, t := range item.Tools {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	b.WriteString("\n" + dimStyle.Render("Instructions:") + "\n")
	b.WriteString(item.Instructions)
	m.detail.SetContent(b.String())
}

// agentsLoadedMsg carries the result of a ListAgents call.
type agentsLoadedMsg struct {
	agents []agentDetail
	err    error
}

// agentDeletedMsg carries the result of a DeleteAgent call.
type agentDeletedMsg struct {
	name string
	err  error
}

func (m agentsTabModel) load() tea.Cmd {
	c, ctx := m.client, m.ctx
	return func() tea.Msg {
		agents, err := c.ListAgents(ctx)
		return agentsLoadedMsg{agents: agents, err: err}
	}
}

func (m agentsTabModel) Update(msg tea.Msg) (agentsTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case agentsLoadedMsg:
		m.loading, m.loaded = false, true
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.agents = msg.agents
		items := make([]list.Item, len(m.agents))
		for i, a := range m.agents {
			items[i] = agentItem{agentDetail: a}
		}
		cmd := m.list.SetItems(items)
		m.syncDetail()
		return m, cmd

	case agentDeletedMsg:
		m.status = ""
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		return m, m.load()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m agentsTabModel) handleKey(msg tea.KeyMsg) (agentsTabModel, tea.Cmd) {
	key := msg.String()
	if key != "x" && m.armDelete != "" {
		m.armDelete = ""
		m.status = ""
	}

	switch key {
	case "r":
		m.loading = true
		m.err = nil
		return m, m.load()

	case "enter":
		item, ok := m.list.SelectedItem().(agentItem)
		if !ok {
			return m, nil
		}
		agent := item.Name
		return m, func() tea.Msg { return switchAgentMsg{agent: agent} }

	case "x":
		item, ok := m.list.SelectedItem().(agentItem)
		if !ok {
			return m, nil
		}
		if item.ReadOnly {
			m.err = fmt.Errorf("can't delete a read-only default agent")
			return m, nil
		}
		if m.armDelete == item.Name {
			m.armDelete = ""
			m.status = ""
			c, ctx := m.client, m.ctx
			name := item.Name
			return m, func() tea.Msg {
				err := c.DeleteAgent(ctx, name)
				return agentDeletedMsg{name: name, err: err}
			}
		}
		m.armDelete = item.Name
		m.status = fmt.Sprintf("delete %q -- press x again to confirm", item.Name)
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.syncDetail()
	return m, cmd
}

func (m agentsTabModel) View() string {
	left := m.list.View()
	// No .Width() here: m.detail.View() is already exactly m.detail.Width
	// columns wide (the viewport handles that itself), and re-applying a
	// Width on this bordered/padded style would word-wrap it a second
	// time at a narrower effective width (width minus this style's own
	// padding) -- silently turning any full-width line into two lines and
	// overflowing the tab's height budget by exactly the number of lines
	// that happened to fill the viewport's full width.
	right := paneBorderStyle.Height(m.detail.Height).Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	var status string
	switch {
	case m.err != nil:
		status = errStyle.Render("error: " + m.err.Error())
	case m.status != "":
		status = promptStyle.Render(m.status)
	case m.loading:
		status = dimStyle.Render("loading...")
	case m.loaded && len(m.agents) == 0:
		status = dimStyle.Render("no agents found")
	default:
		status = dimStyle.Render("enter: use in chat  x: delete  r: refresh")
	}
	return fmt.Sprintf("%s\n%s", body, status)
}
