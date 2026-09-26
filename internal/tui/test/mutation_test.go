package tui_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

const (
	shippedWhere      = `o.status = "shipped"`
	archiveShipped    = `UPDATE sales.orders o SET o.status = "archived" WHERE ` + shippedWhere
	updateReviewTitle = " Review update · "
	progressTitle     = " Update · "
	updatedResults    = "Results · mock · updated"
)

func shippedOrder(id, customer, status string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"customerId":%q,"status":%q,"total":%d}`, id, customer, status, len(id)))
}

// updateStore is what an update's tests start from: four shipped orders
// and an open one in sales.orders, and the predicates their statements use.
func updateStore(opts ...mock.Option) []mock.Option {
	statusIs := func(status string) func(json.RawMessage) bool {
		return func(item json.RawMessage) bool {
			var head struct {
				Status string `json:"status"`
			}
			return json.Unmarshal(item, &head) == nil && head.Status == status
		}
	}
	return append([]mock.Option{
		mock.WithPredicate(shippedWhere, statusIs("shipped")),
		mock.WithPredicate("true", func(json.RawMessage) bool { return true }),
		mock.WithItems(ordersPath,
			shippedOrder("o1", "c01", "shipped"), shippedOrder("o2", "c01", "shipped"),
			shippedOrder("o3", "c02", "shipped"), shippedOrder("o4", "c02", "shipped"),
			shippedOrder("o5", "c03", "open")),
	}, opts...)
}

func newUpdateConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	return newConnection(t, updateStore(opts...)...)
}

func newUpdateModel(t *testing.T, conn *recordingConnection, store history.Store, accounts ...tui.Account) tea.Model {
	t.Helper()
	m, _ := newModelWith(t, conn, tui.Options{Manage: managed, History: store, Accounts: accounts}).
		Update(tea.WindowSizeMsg{Width: 3 * testWidth, Height: testHeight})
	return settleNow(m, m.Init())
}

// dryRun types statement and presses ctrl+r, driving the selection to its
// end.
func dryRun(t *testing.T, m tea.Model, statement string) tea.Model {
	t.Helper()
	return pressNow(t, typeQuery(t, m, statement), keyMsg(tea.KeyCtrlR))
}

// rerun replaces the text of the editor, which has the keyboard, and runs
// it.
func rerun(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return pressNow(t, pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText(text)), keyMsg(tea.KeyCtrlR))
}

// confirmUpdate types the confirmation and presses enter, driving the job
// to its end.
func confirmUpdate(t *testing.T, m tea.Model, typed string) tea.Model {
	t.Helper()
	return pressNow(t, pressAll(t, m, keyText(typed)), keyMsg(tea.KeyEnter))
}

// heldConfirm types the confirmation and presses enter, and hands back the
// job's first step without running it, so the test decides when the job moves.
func heldConfirm(t *testing.T, m tea.Model, typed string) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, m, keyText(typed))
	m, step := m.Update(keyMsg(tea.KeyEnter))
	require.Contains(t, plain(m.View()), progressTitle)
	return m, step
}

func statuses(t *testing.T, conn *recordingConnection) map[string]string {
	t.Helper()
	listed := map[string]string{}
	for _, item := range conn.store.Items(ordersPath) {
		var head struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		require.NoError(t, json.Unmarshal(item, &head))
		listed[head.ID] = head.Status
	}
	return listed
}

func TestCtrlROnAnUpdateReadsAndOpensTheReviewWritingNothing(t *testing.T) {
	conn := newUpdateConnection(t)

	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)

	view := plain(m.View())
	for _, want := range []string{updateReviewTitle + "mock", "sales.orders", "key /customerId", shippedWhere,
		"4 matched at", `set /status "archived"`, "o1  \"c01\"", `~ /status`, `"shipped" → "archived"`,
		"… 1 more", "Type the container name to update 4 items:"} {
		assert.Contains(t, view, want)
	}
	assert.Positive(t, conn.scans)
	assert.Zero(t, conn.edits.Load())
}

func TestOnlyTheExactNameStartsTheUpdate(t *testing.T) {
	for _, typed := range []string{"", "Orders", "orders ", "sales.orders"} {
		t.Run(fmt.Sprintf("%q", typed), func(t *testing.T) {
			conn := newUpdateConnection(t)
			m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)

			m = confirmUpdate(t, m, typed)

			assert.Contains(t, plain(m.View()), updateReviewTitle)
			assert.Zero(t, conn.edits.Load())
		})
	}
	t.Run("the name", func(t *testing.T) {
		conn := newUpdateConnection(t)
		m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)

		m = confirmUpdate(t, m, firstContainer)

		assert.Contains(t, plain(m.View()), progressTitle+"mock / sales.orders")
		assert.Contains(t, plain(m.View()), "Done.")
		assert.Equal(t, int32(4), conn.edits.Load())
		assert.Equal(t, map[string]string{"o1": "archived", "o2": "archived", "o3": "archived", "o4": "archived", "o5": "open"}, statuses(t, conn))
	})
}

func TestEveryItemNeedsTheNameAndTheCount(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), `UPDATE sales.orders o SET o.flag = true WHERE true`)
	require.Contains(t, plain(m.View()), "Every item in sales.orders. Type the container name and the item count:")
	require.Contains(t, plain(m.View()), "WHERE true: every item in sales.orders is a target.")

	m = confirmUpdate(t, m, firstContainer)
	assert.Zero(t, conn.edits.Load(), "the name alone starts nothing")

	confirmUpdate(t, m, " 5")
	assert.Equal(t, int32(5), conn.edits.Load())
}

func TestEscClosesTheUpdateReviewHavingWrittenAndRecordedNothing(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}

	m := pressAll(t, dryRun(t, newUpdateModel(t, conn, store), archiveShipped), keyMsg(tea.KeyEscape))

	view := plain(m.View())
	assert.NotContains(t, view, updateReviewTitle)
	assert.Contains(t, view, "UPDATE sales.orders o", "the buffer is left as it was")
	assert.Zero(t, conn.edits.Load())
	assert.Empty(t, store.entries)
}

func TestRecallAndRunReviewsAgain(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := confirmUpdate(t, dryRun(t, newUpdateModel(t, conn, store), `UPDATE sales.orders o SET o.flag = true WHERE true`), "orders 5")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	require.Len(t, store.entries, 1)
	edits := conn.edits.Load()

	m = pressAll(t, m, keyMsg(tea.KeyCtrlO))
	m = pressNow(t, m, keyMsg(tea.KeyCtrlR))

	assert.Contains(t, plain(m.View()), updateReviewTitle, "a rerun selects afresh and asks again")
	assert.Equal(t, edits, conn.edits.Load())
}

func TestAReadOnlyAccountRefusesAnUpdateBeforeReadingAnything(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}

	m := dryRun(t, newUpdateModel(t, conn, store, tui.Account{Name: mock.Name, ReadOnly: true}), archiveShipped)

	view := plain(m.View())
	assert.Contains(t, view, "mock is read-only, so nothing was sent.")
	assert.Contains(t, view, "alchemist profile set-read-only mock false")
	assert.Zero(t, conn.scans)
	assert.Zero(t, conn.edits.Load())
	require.Len(t, store.entries, 1)
	assert.Equal(t, history.KindUpdate, store.entries[0].Kind)
}

func TestAConnectionThatCannotEditItemsRefusesAnUpdate(t *testing.T) {
	conn := newUpdateConnection(t)
	manage := func(c adapter.Connection) tui.Management {
		management := managed(c)
		management.Editor = nil
		return management
	}
	m := newModelWith(t, conn, tui.Options{Manage: manage})
	m, _ = settle(m, m.Init())

	m = dryRun(t, m, archiveShipped)

	assert.Contains(t, plain(m.View()), "this adapter cannot update or delete items")
	assert.Zero(t, conn.scans)
}

func TestARefusedUpdateListsEveryProblemAndReachesNoAdapter(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}

	m := dryRun(t, newUpdateModel(t, conn, store), `UPDATE sales.orders o SET o.id = "x", o.customerId = "c9" WHERE true`)

	view := plain(m.View())
	assert.Contains(t, view, "Update refused, nothing was read:")
	assert.Contains(t, view, "cannot change id")
	assert.Contains(t, view, "cannot change the partition key /customerId")
	assert.Zero(t, conn.scans)
	require.Len(t, store.entries, 1)
	assert.NotEmpty(t, store.entries[0].Error)
}

func TestAnUpdateThatDoesNotParseSaysWhere(t *testing.T) {
	conn := newUpdateConnection(t)

	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), `UPDATE sales.orders o SET o.status = "x"`)

	assert.Contains(t, plain(m.View()), "UPDATE needs a WHERE. To change every item, write WHERE true")
	assert.Zero(t, conn.scans)
}

func TestMoreMatchesThanTheLimitIsARefusal(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}

	m := dryRun(t, newUpdateModel(t, conn, store, tui.Account{Name: mock.Name, MaxMutationItems: 3}), archiveShipped)

	view := plain(m.View())
	assert.Contains(t, view, "more than 3 items match")
	assert.Contains(t, view, "raise max_mutation_items on the mock profile")
	assert.NotContains(t, view, updateReviewTitle)
	assert.Zero(t, conn.edits.Load())
	require.Len(t, store.entries, 1)
}

func TestNoMatchIsARecordedResult(t *testing.T) {
	conn := newUpdateConnection(t, mock.WithPredicate(`o.status = "lost"`, func(json.RawMessage) bool { return false }))
	store := &recordingStore{}

	m := dryRun(t, newUpdateModel(t, conn, store), `UPDATE sales.orders o SET o.status = "archived" WHERE o.status = "lost"`)

	assert.Contains(t, plain(m.View()), "No item in sales.orders matches. Nothing to update.")
	assert.NotContains(t, plain(m.View()), updateReviewTitle)
	require.Len(t, store.entries, 1)
	assert.True(t, store.entries[0].OK)
	assert.Zero(t, store.entries[0].Rows)
}

func TestTheJobRunsBehindAHiddenViewAndReportsWhenClosed(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)
	m, step := heldConfirm(t, m, firstContainer)

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.NotContains(t, plain(m.View()), progressTitle, "esc hides the view")
	assert.Contains(t, statusBar(m), "update mock/sales.orders 0% (w)")
	m = rerun(t, m, "SELECT * FROM sales.orders")
	assert.Contains(t, plain(m.View()), "item-1-0", "a query runs meanwhile")
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('w'))
	require.Contains(t, plain(m.View()), progressTitle, "w reopens the view from the catalog")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))
	assert.NotContains(t, plain(m.View()), " Accounts ", "the view swallows ctrl+g")
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = settleNow(m, step)
	assert.Contains(t, statusBar(m), "update done (w)")
	m = pressAll(t, m, keyRune('w'))
	require.Contains(t, plain(m.View()), "Done.")
	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, updatedResults)
	assert.Contains(t, view, "Updated 4 of 4 items in sales.orders.")
	assert.NotContains(t, statusBar(m), "(w)", "the slot is free once the view is closed")
	assert.Contains(t, statusBar(m), "4 items")
	assert.Contains(t, statusBar(m), "(selection 5.00 + writes 40.00)")
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyMsg(tea.KeyEnter))
	assert.Contains(t, plain(m.View()), `"outcome": "updated"`, "enter opens a report row's detail")
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyCtrlE))
	assert.Contains(t, plain(m.View()), "Export", "ctrl+e exports the report")
}

func TestWWithNoJobDoesNothingAndIsNotInHelp(t *testing.T) {
	m := newUpdateModel(t, newUpdateConnection(t), &recordingStore{})

	m = pressAll(t, m, keyRune('w'))
	assert.NotContains(t, plain(m.View()), progressTitle)
	m = pressAll(t, m, keyRune('?'))
	assert.NotContains(t, plain(m.View()), "show update/delete job")
}

func TestAStopThenAResumeWritesEachItemOnce(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := dryRun(t, newUpdateModel(t, conn, store), archiveShipped)
	m, step := heldConfirm(t, m, firstContainer)

	m = pressAll(t, m, keyRune('x'))
	assert.Contains(t, plain(m.View()), "Stopping after the writes in flight…")
	m = settleNow(m, step)
	require.Contains(t, plain(m.View()), "Stopped.")
	assert.Contains(t, plain(m.View()), "4 were not attempted and are unchanged.")
	assert.Zero(t, conn.edits.Load(), "the stop came before the first write started")

	m = pressNow(t, m, keyRune('r'))
	require.Contains(t, plain(m.View()), "Done.")
	assert.Equal(t, int32(4), conn.edits.Load())
	assert.ElementsMatch(t, []string{"o1", "o2", "o3", "o4"}, conn.store.EditedIDs())
	pressAll(t, m, keyMsg(tea.KeyEnter))
	require.Len(t, store.entries, 1)
	assert.True(t, store.entries[0].OK)
	assert.Equal(t, 4, store.entries[0].Rows)
}

func TestAStoppedUpdateIsRecordedAsSuch(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := dryRun(t, newUpdateModel(t, conn, store), archiveShipped)
	m, step := heldConfirm(t, m, firstContainer)
	m = settleNow(pressAll(t, m, keyRune('x')), step)

	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	assert.Contains(t, plain(m.View()), "Results · mock · stopped")
	assert.Contains(t, plain(m.View()), "4 were not attempted and are unchanged.")
	require.Len(t, store.entries, 1)
	entry := store.entries[0]
	assert.False(t, entry.OK)
	assert.Equal(t, "stopped after 0 of 4", entry.Error)
	assert.Equal(t, history.KindUpdate, entry.Kind)
	assert.Equal(t, ordersPath, entry.Scope)
}

func TestAnUpdateWithAFailedItemOpensOnIt(t *testing.T) {
	conn := newUpdateConnection(t, mock.WithEditUnknown("o3"))

	m := confirmUpdate(t, dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped), firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, "Updated 3 of 4 items in sales.orders. 1 had no answer.")
	assert.Contains(t, view, "no answer: check before")
	assert.Contains(t, view, "unknown")
}

func TestQuittingARunningUpdateWarnsFirstAndRecordsIt(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := dryRun(t, newUpdateModel(t, conn, store), archiveShipped)
	m, _ = heldConfirm(t, m, firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab))

	m, msgs := press(t, m, keyRune('q'))
	require.False(t, hasMsg[tea.QuitMsg](msgs))
	assert.Contains(t, plain(m.View()), "Quit again to stop it and quit")

	_, msgs = press(t, m, keyRune('q'))
	assert.True(t, hasMsg[tea.QuitMsg](msgs))
	require.Len(t, store.entries, 1)
	assert.False(t, store.entries[0].OK)
	assert.Contains(t, store.entries[0].Error, "stopped after 0 of 4")
}

func TestOneJobAtATime(t *testing.T) {
	conn := newUpdateConnection(t)
	m := newWideModel(t, conn, tui.Options{Manage: managed, Snapshots: t.TempDir(),
		Accounts: []tui.Account{{Name: mock.Name, Database: firstDatabase}}})
	m = dryRun(t, m, archiveShipped)
	m, _ = heldConfirm(t, m, firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	scans := conn.scans

	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('y'))
	assert.Contains(t, statusBar(m), "an update is running: clones wait for it (w)")
	m = pressAll(t, m, keyRune('s'))
	assert.Contains(t, statusBar(m), "an update is running: snapshots wait for it (w)")
	m = rerun(t, pressAll(t, m, keyRune('e')), archiveShipped)
	assert.Contains(t, plain(m.View()), "an update is running on mock/sales.orders: w in the catalog shows it")
	assert.Equal(t, scans, conn.scans)
}

func TestABatchIntoTheUpdatedContainerWaits(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)
	m, _ = heldConfirm(t, m, firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = rerun(t, m, `BEGIN BATCH sales.orders PARTITION "c01"; DELETE "o1"; COMMIT`)
	assert.Contains(t, plain(m.View()), "an update is writing sales.orders: the batch waits for it (w)")
	m = rerun(t, m, `BEGIN BATCH sales.customers PARTITION "west"; CREATE {"id": "k1", "region": "west"}; COMMIT`)
	assert.Contains(t, plain(m.View()), " Review batch ", "a batch into another container is reviewed as usual")
	assert.Empty(t, conn.batches)
}

func TestAnUpdateWaitsForAClone(t *testing.T) {
	o := newOpener(t)
	o.options["prod"] = updateStore()
	m := newCloneAccountsModel(t, o)
	m, _ = cloneToStaging(t, m)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	prod := o.last("prod")
	scans := prod.scans

	m = dryRun(t, m, archiveShipped)

	assert.Contains(t, plain(m.View()), "a clone is running: updates wait for it (y)")
	assert.Equal(t, scans, prod.scans, "refused before the dry run spends anything")
}

func TestAJobStaysOnTheAccountItWasConfirmedOn(t *testing.T) {
	o := newOpener(t)
	o.options["prod"] = updateStore()
	o.options["staging"] = updateStore()
	store := &recordingStore{}
	m, _ := tui.New(tui.Options{
		Icons: theme.Icons(), Accounts: fixtureAccounts(), Launch: "prod", Open: o.open, Manage: managed, History: store,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	m = dryRun(t, m, archiveShipped)
	m, step := heldConfirm(t, m, firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	m = switchTo(t, m, "staging")
	onAccount(t, m, "staging")

	m = settleNow(m, step)
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('w'), keyMsg(tea.KeyEnter))

	assert.Equal(t, int32(4), o.last("prod").edits.Load())
	assert.Zero(t, o.last("staging").edits.Load())
	require.Len(t, store.entries, 1)
	assert.Equal(t, "prod", store.entries[0].Profile)
	assert.Contains(t, plain(m.View()), "Results · prod · updated")
}

func TestTheSwitcherKeepsTheJobsAccountConnected(t *testing.T) {
	o := newOpener(t)
	o.options["prod"] = updateStore()
	m, _ := tui.New(tui.Options{
		Icons: theme.Icons(), Accounts: fixtureAccounts(), Launch: "prod", Open: o.open, Manage: managed,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	m = dryRun(t, m, archiveShipped)
	m, _ = heldConfirm(t, m, firstContainer)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	m = switchTo(t, m, "staging")

	m = pressAll(t, highlight(t, pressAll(t, m, keyMsg(tea.KeyCtrlG)), "prod"), keyRune('x'))
	assert.Contains(t, plain(m.View()), "an update is using prod: stop it first (w in the catalog)")
	m = pressAll(t, highlight(t, m, "staging"), keyRune('x'))
	assert.NotContains(t, plain(m.View()), "staging · connected")
}

func TestASwitchWhileSelectingLeavesTheReviewOnItsAccount(t *testing.T) {
	o := newOpener(t)
	o.options["prod"] = updateStore()
	m, _ := tui.New(tui.Options{
		Icons: theme.Icons(), Accounts: fixtureAccounts(), Launch: "prod", Open: o.open, Manage: managed,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	m = typeQuery(t, m, archiveShipped)
	m, selection := m.Update(keyMsg(tea.KeyCtrlR))
	require.Contains(t, statusBar(m), "selecting…")

	m = switchTo(t, m, "staging")
	m = settleNow(m, selection)

	view := plain(m.View())
	assert.Contains(t, view, updateReviewTitle+"prod")
	assert.Contains(t, view, "Account        prod")
}

func TestEscCancelsASelection(t *testing.T) {
	conn := newUpdateConnection(t)
	m := typeQuery(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)
	m, selection := m.Update(keyMsg(tea.KeyCtrlR))

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	m = settleNow(m, selection)

	assert.Contains(t, plain(m.View()), "selection cancelled: nothing was written")
	assert.NotContains(t, plain(m.View()), updateReviewTitle, "the page that was in flight is dropped")
}

func TestAResumeOnAnAccountTurnedReadOnlyWritesNothing(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)
	m, step := heldConfirm(t, m, firstContainer)
	m = settleNow(pressAll(t, m, keyRune('x')), step)
	m, _ = m.Update(tui.AccountsListedMsg{Accounts: []tui.Account{{Name: mock.Name, ReadOnly: true}}})

	m = pressNow(t, m, keyRune('r'))

	assert.Contains(t, plain(m.View()), "mock is read-only, so nothing was sent.")
	assert.Zero(t, conn.edits.Load())
}

func TestTheReviewAndTheProgressFitTheSmallestTerminal(t *testing.T) {
	conn := newUpdateConnection(t)
	m := newModelWith(t, conn, tui.Options{Manage: managed})
	m, _ = settle(m, m.Init())

	m = dryRun(t, m, archiveShipped)
	view := plain(m.View())
	assert.Contains(t, view, "Type the container name to update 4 items:")
	assert.Contains(t, view, "> ", "the confirmation stays in view")
	assert.Contains(t, view, "enter start")
	m, _ = heldConfirm(t, m, firstContainer)
	view = plain(m.View())
	assert.Contains(t, view, "Writers")
	assert.Contains(t, view, "esc hide")
}

func TestAnAccountTurnedReadOnlyMidJobWritesNoFurtherChunk(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), archiveShipped)
	m, probe := heldConfirm(t, m, firstContainer)
	answered := answers(probe)
	require.Equal(t, int32(1), conn.edits.Load(), "the probe wrote its one item")

	m, _ = m.Update(tui.AccountsListedMsg{Accounts: []tui.Account{{Name: mock.Name, ReadOnly: true}}})
	for _, msg := range answered {
		m = settleNow(m.Update(msg))
	}

	view := plain(m.View())
	assert.Contains(t, view, "Failed.")
	assert.Contains(t, view, "mock is read-only, so nothing was sent.")
	assert.Contains(t, view, "r resume", "the job ends short, resumable")
	assert.Equal(t, int32(1), conn.edits.Load(), "no chunk after the profile turned read-only")
}

func TestTheRowsPastTheReportsCapAreLogged(t *testing.T) {
	total := mutate.MaxReportRows + 2
	conn := newConnection(t, mock.WithPredicate("true", func(json.RawMessage) bool { return true }), mock.WithItemCount(ordersPath, total))
	var logged bytes.Buffer
	m := newWideModel(t, conn, tui.Options{
		Manage: managed, Logger: log.New(&logged),
		Accounts: []tui.Account{{Name: mock.Name, MaxMutationItems: 2 * total, Writers: 16}},
	})
	m = dryRun(t, m, `UPDATE sales.orders o SET o.x = 1 WHERE true`)

	m = confirmUpdate(t, m, fmt.Sprintf("orders %d", total))
	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "The log names the rest.")
	assert.Equal(t, 2, strings.Count(logged.String(), "update outcome past the report's rows"))
	assert.Contains(t, logged.String(), "item-10001")
}
