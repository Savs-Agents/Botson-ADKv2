package chat

import "github.com/charmbracelet/lipgloss"

// Style sheet for the dashboard. Sticks to the existing ANSI 256 palette
// (colors 8/9/11 were already in use by the original single-view chat) and
// extends it modestly rather than introducing truecolor, so it still reads
// correctly on a plain 256-color terminal.
var (
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))             // red
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // gray
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))             // green
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))             // blue

	// Tab bar.
	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("4")).
			Bold(true).
			Padding(0, 2)
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Padding(0, 2)
	tabBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("8"))

	// Master/detail panes (Agents tab).
	paneBorderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("4")).
				Bold(true)
	badgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")).
			Padding(0, 1)

	// Stat tiles (Stats tab).
	statTileStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 2).
			MarginRight(2)
	statValueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	statLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Footer/help bar.
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
