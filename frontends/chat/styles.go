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

	// Chat tab's status bar (top) and input box (bottom): horizontal-rule
	// borders only (no left/right sides), matching the tab bar's own
	// bottom-rule look rather than introducing a boxed/vertical-border
	// style elsewhere in the dashboard. The status bar only needs a
	// bottom rule -- the tab bar above it already draws the separating
	// line above; drawing another directly below that would double up.
	// The input box gets both, so it reads as a distinct, fully-bracketed
	// box separating it from the transcript above and the help footer
	// below.
	chatStatusBarStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(lipgloss.Color("8"))
	chatInputBoxStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
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

// truncateToWidth hard-truncates s (ANSI-aware, so already-styled text
// truncates correctly) to at most width columns, with no wrapping. Used
// before handing content to a Style with .Width() set for a border --
// Style.Width() word-*wraps* content that's too long rather than
// truncating it, which would silently turn one line into two and
// overflow a tab's fixed height budget by exactly that much (the same
// class of bug the Agents tab's detail pane hit once already).
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
