package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// openDatabaseForm opens the new-database dialog. Where the cursor sits does
// not matter: a database has no parent to land in.
func (m Model) openDatabaseForm() Model {
	return m.showForm(panes.NewDatabaseForm(m.icons), OpCreateDatabase, nil)
}

// openContainerForm creates in the database under the cursor, or in the one
// holding the container under it.
func (m Model) openContainerForm() Model {
	node, ok := m.catalogPane().SelectedNode()
	if !ok {
		return m
	}
	database := node.Path[0]
	return m.showForm(panes.NewContainerForm(m.icons, database), OpCreateContainer, []string{database})
}

// openDelete refuses anything a background job is writing to, whichever
// account the session is on.
func (m Model) openDelete() (Model, tea.Cmd) {
	node, ok := m.catalogPane().SelectedNode()
	if !ok {
		return m, nil
	}
	if m.job.writesTo(m.accounts.active, node.Path) {
		return m.notify(m.job.writingText(node.Path))
	}
	if node.Kind == adapter.NodeContainer {
		return m.showConfirm(panes.NewContainerDelete(m.icons, node.Path), OpDeleteContainer, node.Path), nil
	}
	return m.showConfirm(panes.NewDatabaseDelete(m.icons, node.Name), OpDeleteDatabase, node.Path), nil
}

// openThroughput reads what the node under the cursor provisions now. The
// dialog opens on the answer, so it never seeds itself with a stale figure.
func (m Model) openThroughput() tea.Cmd {
	node, ok := m.catalogPane().SelectedNode()
	if !ok {
		return nil
	}
	return m.readThroughput(node.Path)
}

func (m Model) openThroughputForm(msg ThroughputReadMsg) Model {
	form := panes.NewThroughputForm(m.icons, msg.Path, msg.Throughput)
	return m.showForm(form, OpSetThroughput, msg.Path)
}

// failManagement puts a refusal under the fields of the dialog that asked for
// it, and nowhere else: a reply to a question the user has walked away from
// must not land on the one now on screen, however alike the two look.
func (m Model) failManagement(msg ErrMsg) Model {
	if msg.dialog != m.dialog {
		return m
	}
	if deletes(msg.Op) {
		m.confirm = m.confirm.Fail(msg.Err)
		return m
	}
	m.form = m.form.Fail(msg.Err)
	return m
}

// failThroughputRead opens the dialog regardless, with the reason the current
// capacity could not be read under its fields; the alternative is a key that
// silently does nothing. A read that did not answer rules nothing out, so
// every mode stays on offer and the service has the last word.
func (m Model) failThroughputRead(msg ErrMsg) Model {
	if msg.Account != m.accounts.active {
		return m
	}
	unknown := adapter.Throughput{Mode: adapter.ThroughputManual}
	m = m.openThroughputForm(ThroughputReadMsg{Account: msg.Account, Path: msg.Path, Throughput: unknown})
	m.form = m.form.Fail(msg.Err)
	return m
}

func (m Model) showForm(form panes.Form, op string, target []string) Model {
	m.form = form.SetSize(m.width, m.height)
	m.managing, m.target, m.dialog = op, target, m.dialog+1
	m.overlay = overlayForm
	return m
}

func (m Model) showConfirm(confirm panes.Confirm, op string, target []string) Model {
	m.confirm = confirm.SetSize(m.width, m.height)
	m.managing, m.target, m.dialog = op, target, m.dialog+1
	m.overlay = overlayConfirm
	return m
}

func (m Model) closeDialog() Model {
	m.overlay = overlayNone
	m.managing, m.target = "", nil
	return m
}

// handleFormKey drives the open dialog. Typed characters belong to its
// fields, which is why q cannot quit here and why a container may be called
// anything at all.
func (m Model) handleFormKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.formUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeDialog(), nil
	case key.Matches(msg, m.keys.Save):
		return m.submitForm()
	case key.Matches(msg, m.keys.NextPane), key.Matches(msg, m.keys.Down):
		m.form = m.form.NextField()
		return m, nil
	case key.Matches(msg, m.keys.PrevPane), key.Matches(msg, m.keys.Up):
		m.form = m.form.PrevField()
		return m, nil
	}
	return m.formUpdate(msg)
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.confirmUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeDialog(), nil
	case key.Matches(msg, m.keys.Save):
		return m.submitConfirm()
	}
	return m.confirmUpdate(msg)
}

func (m Model) formUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m Model) confirmUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	return m, cmd
}

// submitForm sends what the dialog collected, once: a second enter while the
// first is still out would create twice. A form still missing something stays
// open with the reason under its fields, as it does for a refusal.
func (m Model) submitForm() (Model, tea.Cmd) {
	if m.form.Submitting() {
		return m, nil
	}
	if err := m.form.Validate(); err != nil {
		m.form = m.form.Fail(err)
		return m, nil
	}
	m.form = m.form.StartSubmitting()
	switch m.managing {
	case OpCreateDatabase:
		return m, m.createDatabase(adapter.DatabaseSpec{
			Name:       m.form.Value(panes.FieldName),
			Throughput: m.form.Throughput(),
		})
	case OpCreateContainer:
		return m, m.createContainer(adapter.ContainerSpec{
			Database:      m.target[0],
			Name:          m.form.Value(panes.FieldName),
			PartitionKeys: partitionKeyPaths(m.form.Value(panes.FieldPartitionKey)),
			Throughput:    m.form.Throughput(),
		})
	case OpSetThroughput:
		return m, m.setThroughput(m.target, m.form.Throughput())
	}
	return m, nil
}

// submitConfirm deletes only once the name has been typed back exactly, and
// only once however many times enter is pressed.
func (m Model) submitConfirm() (Model, tea.Cmd) {
	if m.confirm.Submitting() || !m.confirm.Confirmed() {
		return m, nil
	}
	m.confirm = m.confirm.StartSubmitting()
	switch m.managing {
	case OpDeleteContainer:
		return m, m.deleteContainer(m.target)
	case OpDeleteDatabase:
		return m, m.deleteDatabase(m.target[0])
	}
	return m, nil
}

// partitionKeyPaths splits a hierarchical key as it is typed: one path, or
// several separated by commas.
func partitionKeyPaths(typed string) []string {
	var paths []string
	for _, path := range strings.Split(typed, ",") {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

// applyChange closes the dialog the mutation came from, reloads the subtree it
// changed in the account it was made on, and leaves the cursor where the
// change puts it. The change itself is applied whatever is on screen — it
// happened — but only the dialog that asked for it is taken down, so one the
// user has since opened survives even when it is the same kind on another
// node.
func (m Model) applyChange(msg CatalogChangedMsg) (Model, tea.Cmd) {
	m.logger.Info("catalog changed", "account", msg.Account, "op", msg.Op, "target", msg.Target)
	if msg.dialog == m.dialog {
		m = m.closeDialog()
	}
	var expiry tea.Cmd
	m.statusBar, expiry = m.statusBar.SetNotice(changeNotice(msg))
	entry, ok := m.accounts.get(msg.Account)
	if msg.Op == OpSetThroughput || !ok || !entry.connected() {
		return m, expiry
	}
	pane := entry.pane
	if msg.Account == m.accounts.active {
		pane = pane.Select(cursorAfter(msg))
	}
	pane, fetch, tick := pane.RefreshPath(msg.Parent)
	entry.pane = pane
	m.accounts.put(entry)

	cmds := []tea.Cmd{tick, expiry}
	if fetch.Needed {
		cmds = append(cmds, m.load(entry, fetch))
	}
	// A scope naming a container that no longer exists would be a lie about
	// what the next run would query.
	if deletes(msg.Op) && within(msg.Target, entry.scope) {
		cmds = append(cmds, scopeChanged(msg.Account, nil))
	}
	return m, tea.Batch(cmds...)
}

// cursorAfter is where a change leaves the cursor: on the node a create made,
// and on the surviving parent of one a delete took away.
func cursorAfter(msg CatalogChangedMsg) []string {
	if deletes(msg.Op) {
		return msg.Parent
	}
	return msg.Target
}

func deletes(op string) bool {
	return op == OpDeleteDatabase || op == OpDeleteContainer
}

// within reports whether path names scope or something scope sits inside.
func within(path, scope []string) bool {
	return len(path) <= len(scope) && slices.Equal(path, scope[:len(path)])
}

func changeNotice(msg CatalogChangedMsg) string {
	target := strings.Join(msg.Target, ".")
	switch msg.Op {
	case OpDeleteDatabase, OpDeleteContainer:
		return "deleted " + target
	case OpSetThroughput:
		return "throughput set on " + target
	}
	return "created " + target
}
