package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/snapshot"
)

// loadDiff opens the store at loc and diffs from and to in it. The store
// stays open while the diff is on screen, for its bodies.
func (m Model) loadDiff(loc snapshot.Location, from, to string) tea.Cmd {
	account, dialog := m.browsing.account, m.dialog
	return func() tea.Msg {
		store, err := snapshot.Open(loc)
		if err != nil {
			return DiffLoadedMsg{Account: account, Location: loc, Err: err, dialog: dialog}
		}
		d, err := store.Diff(from, to)
		if err != nil {
			_ = store.Close() // read-only, and already failing
			return DiffLoadedMsg{Account: account, Location: loc, Err: err, dialog: dialog}
		}
		return DiffLoadedMsg{Account: account, Location: loc, Diff: d, store: store, dialog: dialog}
	}
}

// openDiff shows a diff read for the overlay still open, and asks for the
// fields of the rows on screen.
func (m Model) openDiff(msg DiffLoadedMsg) (Model, tea.Cmd) {
	if msg.dialog != m.dialog || msg.Account != m.browsing.account || !m.browsingSnapshots() {
		return m, closeStore(msg.store)
	}
	if msg.Err != nil {
		m.logger.Error("snapshot diff failed", "store", msg.Location, "error", msg.Err)
		m.snapshotsPane = m.snapshotsPane.SetNotice(msg.Err.Error())
		m.overlay = m.browsing.diffReturn
		return m, nil
	}
	m, closing := m.leaveDiff()
	m.browsing.diffLoc = msg.Location
	m.browsing.store = msg.store
	m.diffPane = m.diffPane.SetDiff(scopeName(msg.Location), msg.Diff)
	m.overlay = overlayDiff
	m, fields := m.requestFields()
	return m, tea.Batch(closing, fields)
}

// browsingSnapshots reports whether a snapshot overlay is on screen.
func (m Model) browsingSnapshots() bool {
	switch m.overlay {
	case overlaySnapshots, overlaySnapshotExport, overlayDiff, overlayGroupDiff, overlayItemDiff:
		return true
	}
	return false
}

// leaveDiff lets go of the diff's store: closed by the command returned if
// the model holds it, or on its way back from the read that holds it.
func (m Model) leaveDiff() (Model, tea.Cmd) {
	store := m.browsing.store
	m.browsing.store, m.browsing.pending = nil, nil
	return m, closeStore(store)
}

func closeStore(store *snapshot.Store) tea.Cmd {
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		_ = store.Close() // read-only: a close failure loses nothing
		return nil
	}
}

// withStore hands the diff's store to read, now or, when another read
// holds it, as soon as that read hands it back. A newer read replaces one
// still waiting.
func (m Model) withStore(read func(*snapshot.Store) tea.Cmd) (Model, tea.Cmd) {
	if m.browsing.store == nil {
		m.browsing.pending = read
		return m, nil
	}
	store := m.browsing.store
	m.browsing.store = nil
	return m, read(store)
}

// takeStoreBack files a store a read hands back, and runs the read waiting
// for it. One read for a diff since left is closed.
func (m Model) takeStoreBack(store *snapshot.Store, dialog dialogID) (Model, tea.Cmd) {
	if store == nil {
		return m, nil
	}
	if dialog != m.dialog || !m.browsingSnapshots() {
		return m, closeStore(store)
	}
	m.browsing.store = store
	if pending := m.browsing.pending; pending != nil {
		m.browsing.pending = nil
		return m.withStore(pending)
	}
	return m, nil
}

// requestFields reads the changed fields of the modified rows on screen
// that are not known yet.
func (m Model) requestFields() (Model, tea.Cmd) {
	unread := m.diffPane.Unread()
	if len(unread) == 0 {
		return m, nil
	}
	dialog := m.dialog
	return m.withStore(func(store *snapshot.Store) tea.Cmd {
		return func() tea.Msg {
			fields := make(map[snapshot.Key]string, len(unread))
			for _, item := range unread {
				names, err := store.ChangedFields(item)
				if err != nil {
					return DiffFieldsMsg{Err: err, store: store, dialog: dialog}
				}
				fields[item.Key] = strings.Join(names, ", ")
			}
			return DiffFieldsMsg{Fields: fields, store: store, dialog: dialog}
		}
	})
}

func (m Model) fileDiffFields(msg DiffFieldsMsg) (Model, tea.Cmd) {
	model, cmd := m.takeStoreBack(msg.store, msg.dialog)
	if msg.dialog != m.dialog {
		return model, cmd
	}
	if msg.Err != nil {
		model.logger.Error("diff fields not read", "store", m.browsing.diffLoc, "error", msg.Err)
		return model, cmd
	}
	model.diffPane = model.diffPane.SetFields(msg.Fields)
	if model.browsing.pending != nil {
		return model, cmd
	}
	model, more := model.requestFields()
	return model, tea.Batch(cmd, more)
}

// handleDiffKey drives the diff. While the filter line has the keyboard,
// typed characters narrow the list, and esc clears it.
func (m Model) handleDiffKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.diffPane.Filtering() && typesIntoBuffer(msg) {
		return m.diffFilterUpdate(msg)
	}
	switch {
	case msg.Type == tea.KeyCtrlC || !m.diffPane.Filtering() && key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close) && m.diffPane.Filtering():
		m.diffPane = m.diffPane.ClearFilter()
		return m.requestFields()
	case key.Matches(msg, m.keys.Close):
		return m.backFromDiff()
	case key.Matches(msg, m.keys.Up):
		m.diffPane = m.diffPane.CursorUp()
		return m.requestFields()
	case key.Matches(msg, m.keys.Down):
		m.diffPane = m.diffPane.CursorDown()
		return m.requestFields()
	case key.Matches(msg, m.keys.CycleChanges):
		m.diffPane = m.diffPane.CycleKind()
		return m.requestFields()
	case key.Matches(msg, m.keys.Filter):
		m.diffPane = m.diffPane.StartFilter()
	case key.Matches(msg, m.keys.OpenChange):
		return m.openSelectedChange()
	case key.Matches(msg, m.keys.Export):
		return m.openDiffExport(), nil
	case m.diffPane.Filtering():
		return m.diffFilterUpdate(msg)
	}
	return m, nil
}

func (m Model) diffFilterUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.diffPane, cmd = m.diffPane.Update(msg)
	model, fields := m.requestFields()
	return model, tea.Batch(cmd, fields)
}

func (m Model) backFromDiff() (Model, tea.Cmd) {
	m.overlay = m.browsing.diffReturn
	return m.leaveDiff()
}

// openSelectedChange is enter on a row: the definition's changes, or the
// item's two bodies side by side.
func (m Model) openSelectedChange() (Model, tea.Cmd) {
	item, definition, ok := m.diffPane.Selected()
	switch {
	case !ok:
		return m, nil
	case definition:
		m.itemDiffPane = m.itemDiffPane.SetDefinition(scopeName(m.browsing.diffLoc), m.diffPane.Diff().Definition)
		m.overlay = overlayItemDiff
		return m, nil
	}
	dialog := m.dialog
	return m.withStore(func(store *snapshot.Store) tea.Cmd {
		return func() tea.Msg {
			before, after, err := store.ItemBodies(item)
			return ItemDiffLoadedMsg{Item: item, Before: before, After: after, Err: err, store: store, dialog: dialog}
		}
	})
}

func (m Model) openItemDiff(msg ItemDiffLoadedMsg) (Model, tea.Cmd) {
	model, cmd := m.takeStoreBack(msg.store, msg.dialog)
	if msg.dialog != m.dialog || model.overlay != overlayDiff {
		return model, cmd
	}
	if msg.Err != nil {
		model.logger.Error("item diff not read", "store", model.browsing.diffLoc, "error", msg.Err)
		return model, cmd
	}
	model.itemDiffPane = model.itemDiffPane.SetItem(msg.Item, msg.Before, msg.After)
	model.overlay = overlayItemDiff
	return model, cmd
}

func (m Model) handleItemDiffKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = overlayDiff
	case key.Matches(msg, m.keys.Up):
		m.itemDiffPane = m.itemDiffPane.ScrollUp()
	case key.Matches(msg, m.keys.Down):
		m.itemDiffPane = m.itemDiffPane.ScrollDown()
	}
	return m, nil
}

func (m Model) loadGroupDiff(from, to string) tea.Cmd {
	loc, dialog := m.browsing.loc, m.dialog
	groups := m.browsing.groups
	return func() tea.Msg {
		first, second, err := findGroups(groups, from, to)
		if err != nil {
			return GroupDiffLoadedMsg{Err: err, dialog: dialog}
		}
		changes, err := snapshot.DiffGroups(loc, first, second)
		return GroupDiffLoadedMsg{From: first, To: second, Changes: changes, Err: err, dialog: dialog}
	}
}

func findGroups(groups []snapshot.Group, from, to string) (snapshot.Group, snapshot.Group, error) {
	var first, second snapshot.Group
	for _, g := range groups {
		switch g.ID {
		case from:
			first = g
		case to:
			second = g
		}
	}
	if first.ID == "" || second.ID == "" {
		return first, second, snapshot.ErrNoSnapshot
	}
	return first, second, nil
}

func (m Model) openGroupDiff(msg GroupDiffLoadedMsg) Model {
	if msg.dialog != m.dialog || m.overlay != overlaySnapshots {
		return m
	}
	if msg.Err != nil {
		m.snapshotsPane = m.snapshotsPane.SetNotice(msg.Err.Error())
		return m
	}
	m.groupDiffPane = m.groupDiffPane.SetChanges(m.browsing.loc.Database, msg.From, msg.To, msg.Changes)
	m.overlay = overlayGroupDiff
	return m
}

func (m Model) handleGroupDiffKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = overlaySnapshots
	case key.Matches(msg, m.keys.Up):
		m.groupDiffPane = m.groupDiffPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.groupDiffPane = m.groupDiffPane.CursorDown()
	case key.Matches(msg, m.keys.DiffSnapshots):
		change, ok := m.groupDiffPane.Selected()
		if !ok {
			return m, nil
		}
		loc := m.browsing.loc
		loc.Container = change.Container
		m.browsing.diffReturn = overlayGroupDiff
		return m, m.loadDiff(loc, change.From, change.To)
	}
	return m, nil
}
