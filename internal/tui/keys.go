package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is the single source of truth for every binding in the TUI. The help
// overlay is generated from FullHelp, so a binding added here needs no
// separate help entry.
type KeyMap struct {
	NextPane    key.Binding
	PrevPane    key.Binding
	FocusEditor key.Binding
	Up          key.Binding
	Down        key.Binding
	Select      key.Binding
	Refresh     key.Binding
	Detail      key.Binding
	ScrollLeft  key.Binding
	ScrollRight key.Binding
	FetchMore   key.Binding
	Run         key.Binding
	History     key.Binding
	Help        key.Binding
	Close       key.Binding
	Quit        key.Binding
}

// DefaultKeyMap binds no function key: too many terminals and laptop
// keyboards deliver them unreliably or not at all.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		NextPane: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next pane"),
		),
		PrevPane: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev pane"),
		),
		FocusEditor: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "editor"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter", " "),
			key.WithHelp("enter/space", "expand/collapse"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh node"),
		),
		Detail: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "row detail"),
		),
		ScrollLeft: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("h/←", "scroll left"),
		),
		ScrollRight: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("l/→", "scroll right"),
		),
		FetchMore: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "fetch more"),
		),
		// ctrl+enter cannot join this binding: terminals send a bare CR for
		// it, so bubbletea would never deliver it.
		Run: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "run query"),
		),
		// History stays disabled — and so stays out of the help overlay —
		// until iteration 7 implements it.
		History: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "history"),
			key.WithDisabled(),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "q"),
			key.WithHelp("q", "quit"),
		),
	}
}

// FullHelp lays the overlay out as one column of keys that work anywhere,
// then a column for each pane that adds its own.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.globalKeys(), k.catalogKeys(), k.resultsKeys()}
}

func (k KeyMap) globalKeys() []key.Binding {
	return []key.Binding{
		k.NextPane, k.PrevPane, k.FocusEditor, k.Run,
		k.History, k.Help, k.Close, k.Quit,
	}
}

func (k KeyMap) catalogKeys() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Refresh}
}

func (k KeyMap) resultsKeys() []key.Binding {
	return []key.Binding{k.Detail, k.ScrollLeft, k.ScrollRight, k.FetchMore}
}

// ShortHelp names the bindings worth a single-line reminder.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPane, k.Select, k.Run, k.Help, k.Quit}
}
