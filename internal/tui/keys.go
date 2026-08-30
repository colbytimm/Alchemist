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
	Run         key.Binding
	History     key.Binding
	Help        key.Binding
	Close       key.Binding
	Quit        key.Binding
}

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
			key.WithKeys("f2"),
			key.WithHelp("f2", "editor"),
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
		// Run and History stay disabled — and so stay out of the help overlay
		// — until iterations 5 and 7 implement them.
		Run: key.NewBinding(
			key.WithKeys("f5", "ctrl+enter"),
			key.WithHelp("f5", "run query"),
			key.WithDisabled(),
		),
		History: key.NewBinding(
			key.WithKeys("f8"),
			key.WithHelp("f8", "history"),
			key.WithDisabled(),
		),
		Help: key.NewBinding(
			key.WithKeys("f1", "?"),
			key.WithHelp("f1/?", "help"),
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

// FullHelp groups the bindings into the columns of the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextPane, k.PrevPane, k.FocusEditor},
		{k.Up, k.Down, k.Select, k.Refresh},
		{k.Run, k.History},
		{k.Help, k.Close, k.Quit},
	}
}

// ShortHelp names the bindings worth a single-line reminder.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPane, k.Select, k.Help, k.Quit}
}

// Bindings flattens FullHelp so callers that iterate the keymap cannot drift
// from what the overlay renders.
func (k KeyMap) Bindings() []key.Binding {
	var all []key.Binding
	for _, group := range k.FullHelp() {
		all = append(all, group...)
	}
	return all
}
