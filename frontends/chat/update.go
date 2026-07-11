package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		const headerHeight, footerHeight = 1, 1
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.viewport.SetContent(strings.Join(m.history, "\n"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - headerHeight - footerHeight
		}
		m.input.Width = msg.Width - 2
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case turnResultMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		lines, pending := renderEvents(msg.events)
		m.history = append(m.history, lines...)
		m.pending = pending
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if m.pending != nil {
		switch strings.ToLower(msg.String()) {
		case "y":
			cmd := m.answerConfirmation(true)
			m.waiting = true
			m.pending = nil
			return m, cmd
		case "n", "esc":
			cmd := m.answerConfirmation(false)
			m.waiting = true
			m.pending = nil
			return m, cmd
		}
		return m, nil // swallow everything else (including enter) while a gated action awaits an explicit y/n
	}

	if m.waiting {
		return m, nil // swallow input while a turn is in flight
	}

	if msg.String() == "enter" {
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.history = append(m.history, "you: "+text)
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		m.input.Reset()
		m.waiting = true
		return m, m.submitMessage(text)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
