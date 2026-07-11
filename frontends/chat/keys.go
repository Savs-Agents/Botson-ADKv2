package chat

import "github.com/charmbracelet/bubbles/key"

// globalKeyMap is handled by the root model before a key is ever handed to
// the active tab -- see model.go's Update. Panel switching deliberately
// uses F1-F5, not plain digits: the Chat tab's text input needs digits to
// type normally, and F-keys are non-printable so they can never collide
// with typed content in any tab (including Settings' form fields).
type globalKeyMap struct {
	Quit        key.Binding
	TabChat     key.Binding
	TabSessions key.Binding
	TabAgents   key.Binding
	TabSettings key.Binding
	TabStats    key.Binding
}

var globalKeys = globalKeyMap{
	Quit:        key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	TabChat:     key.NewBinding(key.WithKeys("f1"), key.WithHelp("f1", "chat")),
	TabSessions: key.NewBinding(key.WithKeys("f2"), key.WithHelp("f2", "sessions")),
	TabAgents:   key.NewBinding(key.WithKeys("f3"), key.WithHelp("f3", "agents")),
	TabSettings: key.NewBinding(key.WithKeys("f4"), key.WithHelp("f4", "settings")),
	TabStats:    key.NewBinding(key.WithKeys("f5"), key.WithHelp("f5", "stats")),
}

// ShortHelp only shows Quit -- the F1-F5 tab bindings are shown inline in
// the tab bar itself (see view.go's renderTabBar) rather than duplicated
// in the footer.
func (k globalKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit}
}

// chatKeyMap is Chat tab-specific; enter/y/n/esc are handled directly by
// string comparison in chat_tab.go (as the original single-view chat
// always did) -- these bindings exist for the help bar, not as the source
// of truth for key matching.
type chatKeyMap struct {
	Send           key.Binding
	Scroll         key.Binding
	NewSession     key.Binding
	ToggleAutoMode key.Binding
	Approve        key.Binding
	Deny           key.Binding
}

var chatKeys = chatKeyMap{
	Send:           key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
	Scroll:         key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
	NewSession:     key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new session")),
	ToggleAutoMode: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "toggle auto-mode")),
	Approve:        key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "approve")),
	Deny:           key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "deny")),
}

func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Scroll, k.NewSession, k.ToggleAutoMode}
}

// sessionsKeyMap is the Sessions tab's own bindings.
type sessionsKeyMap struct {
	Select         key.Binding
	ToggleAutoMode key.Binding
	Delete         key.Binding
	Refresh        key.Binding
}

var sessionsKeys = sessionsKeyMap{
	Select:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open in chat")),
	ToggleAutoMode: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle auto-mode")),
	Delete:         key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete (again to confirm)")),
	Refresh:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
}

func (k sessionsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.ToggleAutoMode, k.Delete, k.Refresh}
}

// agentsKeyMap is the Agents tab's own bindings.
type agentsKeyMap struct {
	Select  key.Binding
	Delete  key.Binding
	Refresh key.Binding
}

var agentsKeys = agentsKeyMap{
	Select:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "use in chat")),
	Delete:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete (again to confirm)")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
}

func (k agentsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Delete, k.Refresh}
}

// settingsKeyMap is the Settings tab's own bindings. Tab/shift+tab move
// field focus here -- safe since panel switching never uses Tab. Refresh
// is ctrl+r, not a plain "r", since every field in this tab is a live
// textinput that needs to accept the letter r as ordinary typed text.
type settingsKeyMap struct {
	Next    key.Binding
	Prev    key.Binding
	Save    key.Binding
	Refresh key.Binding
}

var settingsKeys = settingsKeyMap{
	Next:    key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next field")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "prev field")),
	Save:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
	Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
}

func (k settingsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Prev, k.Save, k.Refresh}
}

// statsKeyMap is the Stats tab's own bindings.
type statsKeyMap struct {
	Select  key.Binding
	Refresh key.Binding
}

var statsKeys = statsKeyMap{
	Select:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open session in chat")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
}

func (k statsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Refresh}
}

// keyMapList adapts a flat slice of bindings to bubbles/help's KeyMap
// interface, so the root view can concatenate globalKeys' bindings with
// whichever tab is active into a single help bar without any tab needing
// to know about globalKeys itself.
type keyMapList []key.Binding

func (k keyMapList) ShortHelp() []key.Binding  { return k }
func (k keyMapList) FullHelp() [][]key.Binding { return [][]key.Binding{k} }
