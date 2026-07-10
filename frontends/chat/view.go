package chat

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m model) View() string {
	if !m.ready {
		return "initializing...\n"
	}

	var footer string
	switch {
	case m.pending != nil:
		footer = promptStyle.Render(fmt.Sprintf("%s [y/n] ", m.pending.hint))
	case m.waiting:
		footer = m.spinner.View() + " thinking..."
	default:
		footer = m.input.View()
	}

	var errLine string
	if m.err != nil {
		errLine = errStyle.Render("error: "+m.err.Error()) + "\n"
	}

	return fmt.Sprintf("%s\n%s%s\n%s",
		m.viewport.View(),
		errLine,
		dimStyle.Render(fmt.Sprintf("— %s (%s) —", m.agent, m.sessionID)),
		footer,
	)
}
