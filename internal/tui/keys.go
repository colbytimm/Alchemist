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
	Clone        key.Binding
	HideClone    key.Binding
	StopClone    key.Binding
	ResumeClone  key.Binding
	DeleteClone  key.Binding
	// ShowMutation reopens the view of an update or delete job; StartMutation and
	// ShowReport work only in the job's own overlays.
	ShowMutation  key.Binding
	StartMutation key.Binding
	ShowReport    key.Binding
	// TakeSnapshot and Snapshots work on the catalog's node; the rest of
	// the snapshot bindings only inside its overlays.
	TakeSnapshot   key.Binding
	Snapshots      key.Binding
	MarkSnapshot   key.Binding
	DiffSnapshots  key.Binding
	DeleteSnapshot key.Binding
	CancelCapture  key.Binding
	ConfirmTake    key.Binding
	ConfirmDelete  key.Binding
	OpenChange     key.Binding
	CycleChanges   key.Binding
	Detail         key.Binding
	ScrollLeft     key.Binding
	ScrollRight    key.Binding
	FetchMore      key.Binding
	Export         key.Binding
	AddToBatch     key.Binding
	Commit         key.Binding
	Scroll         key.Binding
	Save           key.Binding
	Format         key.Binding
	Complete       key.Binding
	Accept         key.Binding
	Run            key.Binding
	Connect        key.Binding
	History        key.Binding
	SaveQuery      key.Binding
	Saved          key.Binding
	Rename         key.Binding
	DeleteQuery    key.Binding
	Confirm        key.Binding
	Accounts       key.Binding
	Switch         key.Binding
	AddAccount     key.Binding
	Disconnect     key.Binding
	Filter         key.Binding
	Recall         key.Binding
	Rerun          key.Binding
	Help           key.Binding
	Close          key.Binding
	Quit           key.Binding
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
		// y also reopens the view of a clone in progress, from any row and
		// any account: one key that means clone in both states.
		Clone: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "clone"),
		),
		HideClone: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "hide"),
		),
		StopClone: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "stop"),
		),
		ResumeClone: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "resume"),
		),
		DeleteClone: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete the partial target"),
		),
		// w means "show the job" while an update or a delete runs, and nothing, absent
		// from help too, while none does.
		ShowMutation: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "show update/delete job"),
		),
		StartMutation: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "start"),
		),
		ShowReport: key.NewBinding(
			key.WithKeys("esc", "enter"),
			key.WithHelp("esc", "report"),
		),
		TakeSnapshot: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "take snapshot"),
		),
		// v also reopens a capture in progress, from any row and any
		// account, as y does a clone.
		Snapshots: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "snapshots"),
		),
		MarkSnapshot: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "mark"),
		),
		DiffSnapshots: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "diff"),
		),
		DeleteSnapshot: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete snapshot"),
		),
		CancelCapture: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "cancel capture"),
		),
		ConfirmTake: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "take"),
		),
		ConfirmDelete: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "delete"),
		),
		OpenChange: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "fields"),
		),
		CycleChanges: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "all/added/removed/modified"),
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
		// The editor's textarea binds ctrl+b to cursor-left, but only while it
		// has the keyboard; this binding lives where the editor does not.
		AddToBatch: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("ctrl+b", "add to batch"),
		),
		Commit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "commit"),
		),
		Scroll: key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("↑/↓", "scroll"),
		),
		Save: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "save"),
		),
		Format: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "json/csv"),
		),
		// ctrl+space reaches bubbletea as ctrl+@, and some terminals swallow
		// it; nothing depends on it, since the list opens on its own.
		Complete: key.NewBinding(
			key.WithKeys("ctrl+@"),
			key.WithHelp("ctrl+space", "complete"),
		),
		Accept: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "accept suggestion"),
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
		// ctrl+s and ctrl+l are bound by neither the editor's textarea nor a
		// text input, so they work while either has the keyboard. bubbletea's
		// raw mode delivers ctrl+s as a key rather than freezing output.
		SaveQuery: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save query"),
		),
		Saved: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "open saved"),
		),
		Rename: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rename"),
		),
		// DeleteQuery is not Delete: that one goes with the catalog's
		// management, and a backend without it can still delete a saved query.
		DeleteQuery: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "delete"),
		),
		// ctrl+g is bound by nothing in the editor's textarea, so it reaches
		// the switcher while the editor has the keyboard.
		Accounts: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "accounts"),
		),
		Switch: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "switch"),
		),
		AddAccount: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add account"),
		),
		Disconnect: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "disconnect"),
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
		{Title: "Editor", Keys: k.editorKeys()},
	}
}

// ConnectKeys are the bindings only the connect form answers to. It shows
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

// SavedKeys are the bindings only the saved queries overlay answers to. Its
// hint line shows Filter, Recall and Rerun in front of them, which
// HistoryKeys already groups.
func (k KeyMap) SavedKeys() []key.Binding {
	return []key.Binding{k.Rename, k.DeleteQuery}
}

// ConfirmKeys answer the saved queries overlay's question before a delete,
// and nothing else.
func (k KeyMap) ConfirmKeys() []key.Binding {
	return []key.Binding{k.Confirm}
}

// AccountsKeys are the bindings only the account switcher answers to. Its
// hint line shows Filter beside them, which HistoryKeys already groups.
func (k KeyMap) AccountsKeys() []key.Binding {
	return []key.Binding{k.Switch, k.AddAccount, k.Disconnect}
}

// ExportKeys are the bindings only the export prompt answers to.
func (k KeyMap) ExportKeys() []key.Binding {
	return []key.Binding{k.Save, k.Format}
}

// BatchKeys are the bindings only the batch review answers to, shown in a
// hint line of its own like ExportKeys. Typed characters go to its name
// field, so none of them is a letter.
func (k KeyMap) BatchKeys() []key.Binding {
	return []key.Binding{k.Commit, k.Scroll}
}

// CloneKeys are the bindings only the clone progress view answers to. It
// shows each in its hint line where it applies, with Close once the clone
// has ended.
func (k KeyMap) CloneKeys() []key.Binding {
	return []key.Binding{k.HideClone, k.StopClone, k.ResumeClone, k.DeleteClone}
}

// MutationReviewKeys are the bindings only a mutation's review answers to.
// Its hint line shows Scroll and Close beside them; typed characters go to
// its confirmation, so none of them is a letter.
func (k KeyMap) MutationReviewKeys() []key.Binding {
	return []key.Binding{k.StartMutation}
}

// MutationProgressKeys are the bindings only a mutation's progress view
// adds to the clone view's HideClone, StopClone and ResumeClone, which mean
// the same there.
func (k KeyMap) MutationProgressKeys() []key.Binding {
	return []key.Binding{k.ShowReport}
}

// SnapshotKeys are the bindings only the snapshot overlays answer to: the
// list, its note prompt and its delete confirmation. Each hint line shows
// the ones that apply, beside TakeSnapshot, Export and Close.
func (k KeyMap) SnapshotKeys() []key.Binding {
	return []key.Binding{k.MarkSnapshot, k.DiffSnapshots, k.DeleteSnapshot, k.CancelCapture, k.ConfirmTake, k.ConfirmDelete}
}

// DiffKeys are the bindings only the diff overlay answers to. Its hint line
// shows Filter and Export beside them.
func (k KeyMap) DiffKeys() []key.Binding {
	return []key.Binding{k.OpenChange, k.CycleChanges}
}

// InfoKeys are the bindings the info overlay answers to, shown in a hint line
// of its own. Every one of them is a binding some pane already advertises.
func (k KeyMap) InfoKeys() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Refresh, k.Close}
}

func (k KeyMap) globalKeys() []key.Binding {
	return []key.Binding{
		k.NextPane, k.PrevPane, k.FocusEditor, k.Run,
		k.History, k.SaveQuery, k.Saved, k.Accounts, k.Help, k.Close, k.Quit,
	}
}

func (k KeyMap) catalogKeys() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Select, k.Refresh, k.Info,
		k.NewDatabase, k.NewContainer, k.Delete, k.Throughput, k.Clone,
		k.TakeSnapshot, k.Snapshots, k.ShowMutation,
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
	if management.Drafter == nil {
		k.AddToBatch.SetEnabled(false)
	}
	if management.Definitions == nil {
		k.Clone.SetEnabled(false)
	}
	if management.Scanner == nil {
		k.TakeSnapshot.SetEnabled(false)
		k.Snapshots.SetEnabled(false)
	}
	return k
}

// withoutSnapshots disables the snapshot bindings for a session with
// nowhere to keep snapshots.
func (k KeyMap) withoutSnapshots() KeyMap {
	k.TakeSnapshot.SetEnabled(false)
	k.Snapshots.SetEnabled(false)
	return k
}

func (k KeyMap) resultsKeys() []key.Binding {
	return []key.Binding{k.Detail, k.ScrollLeft, k.ScrollRight, k.FetchMore, k.Export, k.AddToBatch}
}

func (k KeyMap) editorKeys() []key.Binding {
	return []key.Binding{k.Complete, k.Accept}
}

// ShortHelp names the bindings worth a single-line reminder.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPane, k.Select, k.Run, k.Accounts, k.Help, k.Quit}
}
