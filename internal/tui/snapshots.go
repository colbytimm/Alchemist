package tui

import (
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

var (
	errNothingToCompare = errors.New("nothing to compare: this is the first snapshot")
	itemFormats         = []panes.ExportFormat{
		{Label: "JSON Lines", Extension: snapshot.ExtJSONLines},
		{Label: "JSON", Extension: export.ExtJSON},
	}
	diffFormats = []panes.ExportFormat{
		{Label: "JSON", Extension: export.ExtJSON},
		{Label: "CSV", Extension: export.ExtCSV},
	}
)

// snapshotBrowse is the store the snapshot overlays show: one container's,
// or one database's groups, always of the account it was opened on. Its
// diff's store is held here between reads, and handed to each read in turn.
type snapshotBrowse struct {
	account string
	loc     snapshot.Location
	groups  []snapshot.Group
	// diffLoc is the container of the diff on screen, and diffReturn where
	// esc from it goes back to.
	diffLoc    snapshot.Location
	diffReturn overlay
	// store is nil while a read holds it; pending is the read waiting for
	// it to come back.
	store   *snapshot.Store
	pending func(*snapshot.Store) tea.Cmd
	// exportingDiff is the export prompt writing the diff rather than a
	// snapshot's items.
	exportingDiff bool
}

func (b snapshotBrowse) database() bool { return b.loc.Container == "" }

// openSnapshots is v on the node under the cursor: a container's
// snapshots, or a database's. It does nothing on a field, or where there is
// nowhere to keep snapshots or nothing to read them with.
func (m Model) openSnapshots() (Model, tea.Cmd) {
	entry, connected := m.activeConnection()
	node, selected := m.catalogPane().SelectedNode()
	if !connected || !selected || m.snapshotRoot == "" || entry.management.Scanner == nil || node.Kind == adapter.NodeField {
		return m, nil
	}
	loc := snapshot.Location{Root: m.snapshotRoot, Account: entry.account.Name, Database: node.Path[0]}
	if node.Kind == adapter.NodeContainer {
		loc.Container = node.Path[1]
	}
	return m.openSnapshotsOf(entry.account.Name, loc)
}

// openSnapshotsOf opens the overlay on the store at loc, and reads its
// list. The overlay never switches the session's account.
func (m Model) openSnapshotsOf(account string, loc snapshot.Location) (Model, tea.Cmd) {
	m, closing := m.leaveDiff()
	m.browsing = snapshotBrowse{account: account, loc: loc}
	m.snapshotsPane = m.snapshotsPane.Open(account, scopeName(loc), m.snapshotRoot)
	m.overlay = overlaySnapshots
	m, load := m.syncCapture().reloadSnapshots()
	return m, tea.Batch(closing, load)
}

func scopeName(loc snapshot.Location) string {
	if loc.Container == "" {
		return loc.Database
	}
	return loc.Database + "." + loc.Container
}

// reloadSnapshots reads the list of the store on screen; a list read for
// an overlay opened since is dropped on arrival.
func (m Model) reloadSnapshots() (Model, tea.Cmd) {
	m.dialog++
	browsing, dialog := m.browsing, m.dialog
	return m, func() tea.Msg {
		if browsing.database() {
			return loadGroups(browsing, dialog)
		}
		return loadRecords(browsing, dialog)
	}
}

func loadRecords(browsing snapshotBrowse, dialog dialogID) tea.Msg {
	store, err := snapshot.Open(browsing.loc)
	if err != nil {
		return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
	}
	defer func() { _ = store.Close() }() // read-only: a close failure loses nothing
	usage, err := store.Usage()
	if err != nil {
		return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
	}
	changed := map[string]bool{}
	for _, record := range store.Snapshots() {
		changed[record.ID] = store.DefinitionChanged(record)
	}
	return SnapshotsLoadedMsg{Account: browsing.account, Location: browsing.loc, Records: store.Snapshots(), Changed: changed, Usage: usage, dialog: dialog}
}

func loadGroups(browsing snapshotBrowse, dialog dialogID) tea.Msg {
	groups, err := snapshot.Groups(browsing.loc)
	if err != nil {
		return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
	}
	stores, err := snapshot.Stores(browsing.loc.Root)
	if err != nil {
		return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
	}
	var usage snapshot.Usage
	for _, loc := range stores {
		if loc.Account != browsing.loc.Account || loc.Database != browsing.loc.Database {
			continue
		}
		store, err := snapshot.Open(loc)
		if err != nil {
			return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
		}
		stored, err := store.Usage()
		if err != nil {
			return ErrMsg{Account: browsing.account, Op: OpSnapshot, Err: err, dialog: dialog}
		}
		usage = usage.Add(stored)
	}
	return SnapshotsLoadedMsg{Account: browsing.account, Location: browsing.loc, Groups: groups, Usage: usage, dialog: dialog}
}

// fileSnapshots shows a list read for the overlay still open on its store.
func (m Model) fileSnapshots(msg SnapshotsLoadedMsg) Model {
	if msg.dialog != m.dialog || msg.Account != m.browsing.account || msg.Location != m.browsing.loc {
		return m
	}
	if m.browsing.database() {
		m.browsing.groups = msg.Groups
		m.snapshotsPane = m.snapshotsPane.SetGroups(msg.Groups, panes.FormatUsage(msg.Usage))
		return m
	}
	m.snapshotsPane = m.snapshotsPane.SetRecords(msg.Records, msg.Changed, panes.FormatUsage(msg.Usage))
	return m
}

func (m Model) failSnapshots(msg ErrMsg) Model {
	if msg.dialog != m.dialog {
		return m
	}
	switch m.overlay {
	case overlaySnapshots:
		m.snapshotsPane = m.snapshotsPane.Fail(msg.Err)
	case overlayDiff, overlayGroupDiff, overlayItemDiff:
		m.overlay = overlaySnapshots
		m.snapshotsPane = m.snapshotsPane.SetNotice(msg.Err.Error())
	}
	return m
}

// handleSnapshotsKey drives the overlay. A note prompt takes typed
// characters, and a delete confirmation takes enter and esc alone; so s, d,
// x and q typed here never reach the catalog.
func (m Model) handleSnapshotsKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case m.snapshotsPane.Naming():
		return m.handleNoteKey(msg)
	case m.snapshotsPane.ConfirmingDelete():
		return m.handleDeleteConfirmKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeSnapshots()
	case key.Matches(msg, m.keys.Up):
		m.snapshotsPane = m.snapshotsPane.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.snapshotsPane = m.snapshotsPane.CursorDown()
	case key.Matches(msg, m.keys.MarkSnapshot):
		m.snapshotsPane = m.snapshotsPane.ToggleMark()
	case key.Matches(msg, m.keys.DiffSnapshots):
		return m.diffSelected()
	case m.capturing.running() && key.Matches(msg, m.keys.CancelCapture) && m.showingCapture():
		return m.cancelCapture()
	case key.Matches(msg, m.keys.TakeSnapshot):
		return m.promptInOverlay(), nil
	case !m.browsing.database() && key.Matches(msg, m.keys.DeleteSnapshot):
		m.snapshotsPane = m.snapshotsPane.ConfirmDelete()
	case !m.browsing.database() && key.Matches(msg, m.keys.Export):
		return m.openItemsExport(), nil
	}
	return m, nil
}

// promptInOverlay is s in the overlay, which another job refuses.
func (m Model) promptInOverlay() Model {
	if m.job.active() {
		m.snapshotsPane = m.snapshotsPane.SetNotice(m.job.waitText("snapshots"))
		return m
	}
	m.snapshotsPane = m.snapshotsPane.PromptNote()
	return m
}

func (m Model) handleNoteKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.snapshotsPane = m.snapshotsPane.Settle()
		return m, nil
	case key.Matches(msg, m.keys.ConfirmTake):
		note := m.snapshotsPane.Note()
		m.snapshotsPane = m.snapshotsPane.Settle()
		if m.job.active() {
			return m.promptInOverlay(), nil
		}
		return m.startCapture(note)
	}
	var cmd tea.Cmd
	m.snapshotsPane, cmd = m.snapshotsPane.UpdateNote(msg)
	return m, cmd
}

func (m Model) handleDeleteConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m.quit()
	case key.Matches(msg, m.keys.ConfirmDelete):
		id, _ := m.snapshotsPane.Selected()
		m.snapshotsPane = m.snapshotsPane.Settle()
		return m, deleteSnapshot(m.browsing, id, m.dialog)
	case key.Matches(msg, m.keys.Close):
		m.snapshotsPane = m.snapshotsPane.Settle()
	}
	return m, nil
}

func deleteSnapshot(browsing snapshotBrowse, id string, dialog dialogID) tea.Cmd {
	return func() tea.Msg {
		store, err := snapshot.Open(browsing.loc)
		if err == nil {
			err = store.Delete(id, time.Now())
		}
		return SnapshotDeletedMsg{Account: browsing.account, Location: browsing.loc, ID: id, Err: err, dialog: dialog}
	}
}

func (m Model) finishSnapshotDelete(msg SnapshotDeletedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.logger.Error("snapshot not deleted", "store", msg.Location, "id", msg.ID, "error", msg.Err)
	} else {
		m.logger.Info("snapshot deleted", "store", msg.Location, "id", msg.ID)
	}
	if msg.dialog != m.dialog || m.overlay != overlaySnapshots {
		return m, nil
	}
	if msg.Err != nil {
		m.snapshotsPane = m.snapshotsPane.SetNotice("not deleted: " + msg.Err.Error())
		return m, nil
	}
	return m.reloadSnapshots()
}

// closeSnapshots leaves the overlay. A capture it showed carries on, in
// the status bar, and leaving the quit warning is choosing not to quit.
func (m Model) closeSnapshots() (Model, tea.Cmd) {
	m = m.disarmCaptureQuit()
	m, closing := m.leaveDiff()
	m.overlay = overlayNone
	m.browsing = snapshotBrowse{}
	return m.syncCapture(), closing
}

func (m Model) openItemsExport() Model {
	id, ok := m.snapshotsPane.Selected()
	if !ok {
		return m
	}
	name := m.browsing.loc.Container + "-" + id + snapshot.ExtJSONLines
	m.snapshotExport = m.snapshotExport.WithFormats("Export snapshot "+id, name, itemFormats...).Open()
	m.browsing.exportingDiff = false
	m.overlay = overlaySnapshotExport
	return m
}

func (m Model) openDiffExport() Model {
	d := m.diffPane.Diff()
	name := fmt.Sprintf("%s-%s-%s%s", m.browsing.diffLoc.Container, d.From.ID, d.To.ID, export.ExtJSON)
	m.snapshotExport = m.snapshotExport.WithFormats("Export diff", name, diffFormats...).Open()
	m.browsing.exportingDiff = true
	m.overlay = overlaySnapshotExport
	return m
}

// handleSnapshotExportKey drives the export prompt as the result set's is
// driven, and goes back to the overlay it was opened from.
func (m Model) handleSnapshotExportKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.snapshotExport.Saving() {
		return m.handleSavingKey(msg)
	}
	if typesIntoBuffer(msg) {
		var cmd tea.Cmd
		m.snapshotExport, cmd = m.snapshotExport.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		m.overlay = m.exportReturn()
		return m, nil
	case key.Matches(msg, m.keys.Save):
		return m.saveSnapshotExport()
	case key.Matches(msg, m.keys.Format):
		m.snapshotExport = m.snapshotExport.SwitchFormat()
		return m, nil
	}
	var cmd tea.Cmd
	m.snapshotExport, cmd = m.snapshotExport.Update(msg)
	return m, cmd
}

func (m Model) exportReturn() overlay {
	if m.browsing.exportingDiff {
		return overlayDiff
	}
	return overlaySnapshots
}

func (m Model) saveSnapshotExport() (Model, tea.Cmd) {
	m.snapshotExport = m.snapshotExport.StartSaving()
	target, dialog := m.snapshotExport.Target(), m.dialog
	existing := snapshot.RefuseExisting
	if target.Overwrite {
		existing = snapshot.ReplaceExisting
	}
	if m.browsing.exportingDiff {
		d := m.diffPane.Diff()
		return m.withStore(func(store *snapshot.Store) tea.Cmd {
			return func() tea.Msg {
				path, err := export.ResolvePath(target.Path)
				if err == nil {
					err = store.WriteDiff(path, d, existing)
				}
				return SnapshotExportedMsg{Path: target.Path, Err: err, store: store, dialog: dialog}
			}
		})
	}
	id, _ := m.snapshotsPane.Selected()
	loc := m.browsing.loc
	return m, func() tea.Msg {
		path, err := export.ResolvePath(target.Path)
		if err != nil {
			return SnapshotExportedMsg{Path: target.Path, Err: err, dialog: dialog}
		}
		store, err := snapshot.Open(loc)
		if err != nil {
			return SnapshotExportedMsg{Path: target.Path, Err: err, dialog: dialog}
		}
		defer func() { _ = store.Close() }() // read-only: a close failure loses nothing
		return SnapshotExportedMsg{Path: target.Path, Err: store.WriteItems(path, id, existing), dialog: dialog}
	}
}

func (m Model) finishSnapshotExport(msg SnapshotExportedMsg) (Model, tea.Cmd) {
	model, cmd := m.takeStoreBack(msg.store, msg.dialog)
	if msg.dialog != m.dialog || m.overlay != overlaySnapshotExport {
		return model, cmd
	}
	if msg.Err != nil {
		model.logger.Error("snapshot export failed", "path", msg.Path, "error", msg.Err)
		model.snapshotExport = model.snapshotExport.Fail(msg.Err)
		return model, cmd
	}
	model.logger.Info("snapshot exported", "path", msg.Path)
	model.overlay = model.exportReturn()
	if model.overlay == overlaySnapshots {
		model.snapshotsPane = model.snapshotsPane.SetNotice("exported to " + msg.Path)
	} else {
		model.diffPane = model.diffPane.SetNotice("exported to " + msg.Path)
	}
	return model, cmd
}

// diffSelected is enter: the marked pair, or the row under the cursor
// against the one before it.
func (m Model) diffSelected() (Model, tea.Cmd) {
	from, to, ok := m.snapshotsPane.Pair()
	if !ok {
		m.snapshotsPane = m.snapshotsPane.SetNotice(errNothingToCompare.Error())
		return m, nil
	}
	if m.browsing.database() {
		return m, m.loadGroupDiff(from, to)
	}
	m.browsing.diffReturn = overlaySnapshots
	return m, m.loadDiff(m.browsing.loc, from, to)
}

// disarmCaptureQuit takes back the quit warning, so the next quit warns
// again rather than cancelling the capture.
func (m Model) disarmCaptureQuit() Model {
	m.capturing.quitWarned = false
	m.capturing.status.Warning = ""
	return m
}
