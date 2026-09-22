package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// KeyMap is the single source of truth for every binding in the TUI. The help
// overlay is generated from HelpSections, so a binding added here needs no
// separate help entry.
type KeyMap struct {
	NextPane     key.Binding
	PrevPane     key.Binding
	FocusEditor  key.Binding
	Up           key.Binding
	Down         key.Binding
	Select       key.Binding
	Refresh      key.Binding
	NewDatabase  key.Binding
	NewContainer key.Binding
	Delete       key.Binding
	Throughput   key.Binding
	Info         key.Binding
	Detail       key.Binding
	ScrollLeft   key.Binding
	ScrollRight  key.Binding
	FetchMore    key.Binding
	Export       key.Binding
	Save         key.Binding
	Format       key.Binding
	Run          key.Binding
	Connect      key.Binding
	History      key.Binding
	Filter       key.Binding
	Recall       key.Binding
	Rerun        key.Binding
	Help         key.Binding
	Close        key.Binding
	Quit         key.Binding
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
		NewDatabase: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new database"),
		),
		NewContainer: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "new container"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete node"),
		),
		Throughput: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "throughput"),
		),
		Info: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "node info"),
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
		Export: key.NewBinding(
			key.WithKeys("ctrl+e"),
			key.WithHelp("ctrl+e", "export to file"),
		),
		Save: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "save"),
		),
		Format: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "json/csv"),
		),
		// ctrl+enter cannot join this binding: terminals send a bare CR for
		// it, so bubbletea would never deliver it.
		Run: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "run query"),
		),
		Connect: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "connect"),
		),
		History: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "history"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Recall: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "recall"),
		),
		Rerun: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "recall and run"),
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

// HelpSections lays the overlay out as one column of keys that work
// anywhere, then a column for each pane that adds its own.
func (k KeyMap) HelpSections() []panes.HelpSection {
	return []panes.HelpSection{
		{Title: "Anywhere", Keys: k.globalKeys()},
		{Title: "Catalog", Keys: k.catalogKeys()},
		{Title: "Results", Keys: k.resultsKeys()},
	}
}

// ConnectKeys are the bindings only the connect screen answers to. It shows
// them in a hint line of its own: the help overlay is not reachable from
// there.
func (k KeyMap) ConnectKeys() []key.Binding {
	return []key.Binding{k.Connect}
}

// HistoryKeys are the bindings only the history overlay answers to, shown in
// a hint line of its own like ConnectKeys.
func (k KeyMap) HistoryKeys() []key.Binding {
	return []key.Binding{k.Filter, k.Recall, k.Rerun}
}

// ExportKeys are the bindings only the export prompt answers to.
func (k KeyMap) ExportKeys() []key.Binding {
	return []key.Binding{k.Save, k.Format}
}

// InfoKeys are the bindings the info overlay answers to, shown in a hint line
// of its own. Every one of them is a binding some pane already advertises.
func (k KeyMap) InfoKeys() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Refresh, k.Close}
}

func (k KeyMap) globalKeys() []key.Binding {
	return []key.Binding{
		k.NextPane, k.PrevPane, k.FocusEditor, k.Run,
		k.History, k.Help, k.Close, k.Quit,
	}
}

func (k KeyMap) catalogKeys() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Select, k.Refresh, k.Info,
		k.NewDatabase, k.NewContainer, k.Delete, k.Throughput,
	}
}

// forManagement disables the bindings whose operation this session cannot
// perform. The overlay is generated from the same map, so a disabled binding
// leaves no dead key and no help entry behind.
func (k KeyMap) forManagement(management Management) KeyMap {
	if management.Admin == nil {
		k.NewDatabase.SetEnabled(false)
		k.NewContainer.SetEnabled(false)
		k.Delete.SetEnabled(false)
	}
	if management.Throughput == nil {
		k.Throughput.SetEnabled(false)
	}
	if management.Inspector == nil {
		k.Info.SetEnabled(false)
	}
	return k
}

func (k KeyMap) resultsKeys() []key.Binding {
	return []key.Binding{k.Detail, k.ScrollLeft, k.ScrollRight, k.FetchMore, k.Export}
}

// ShortHelp names the bindings worth a single-line reminder.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPane, k.Select, k.Run, k.Help, k.Quit}
}
