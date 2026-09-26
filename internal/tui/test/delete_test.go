package tui_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/tui"
)

const (
	deleteShipped       = `DELETE FROM sales.orders o WHERE ` + shippedWhere
	deleteReviewTitle   = " Review delete · "
	deleteProgressTitle = " Delete · "
	deleteConfirmation  = "orders 4"
)

// heldDelete types the confirmation and presses enter, and hands back the
// job's first step without running it.
func heldDelete(t *testing.T, m tea.Model, typed string) (tea.Model, tea.Cmd) {
	t.Helper()
	m = pressAll(t, m, keyText(typed))
	m, step := m.Update(keyMsg(tea.KeyEnter))
	require.Contains(t, plain(m.View()), deleteProgressTitle)
	return m, step
}

func storedOrderIDs(t *testing.T, conn *recordingConnection) []string {
	t.Helper()
	var listed []string
	for id := range statuses(t, conn) {
		listed = append(listed, id)
	}
	slices.Sort(listed)
	return listed
}

func TestCtrlROnADeleteReadsAndOpensTheReviewDeletingNothing(t *testing.T) {
	conn := newUpdateConnection(t)

	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)

	view := plain(m.View())
	for _, want := range []string{
		deleteReviewTitle + "mock", "sales.orders", "key /customerId", shippedWhere, "4 matched at",
		`- o1  "c01"  {"status":"shipped","total":2}`, "… 1 more", "roughly 28 RU (7 RU per 1 KB item",
		"Items are deleted one by one. An item changed since", "There is no undo.",
		"Type orders 4 to delete:",
	} {
		assert.Contains(t, view, want)
	}
	assert.NotContains(t, view, "Each gets")
	assert.Positive(t, conn.scans)
	assert.Zero(t, conn.edits.Load())
}

func TestOnlyTheNameAndTheExactCountStartTheDelete(t *testing.T) {
	for _, typed := range []string{"", "orders", "orders 3", "orders  4", "orders 04", "Orders 4", "orders 4 "} {
		t.Run(fmt.Sprintf("%q", typed), func(t *testing.T) {
			conn := newUpdateConnection(t)
			m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)

			m = confirmUpdate(t, m, typed)

			assert.Contains(t, plain(m.View()), deleteReviewTitle)
			assert.Zero(t, conn.edits.Load())
		})
	}
	t.Run("the name and the count", func(t *testing.T) {
		conn := newUpdateConnection(t)
		m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)

		m = confirmUpdate(t, m, deleteConfirmation)

		assert.Contains(t, plain(m.View()), deleteProgressTitle+"mock / sales.orders")
		assert.Contains(t, plain(m.View()), "Done.")
		assert.Contains(t, plain(m.View()), "Items already deleted stay deleted if this stops.")
		assert.Equal(t, int32(4), conn.edits.Load())
		assert.Equal(t, []string{"o5"}, storedOrderIDs(t, conn))
	})
}

func TestTheCountToTypeIsTheCountSelected(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	require.NoError(t, conn.store.PutItem(ordersPath, shippedOrder("o6", "c03", "shipped")))

	m = pressNow(t, m, keyMsg(tea.KeyCtrlR))
	require.Contains(t, plain(m.View()), "5 matched at")
	m = confirmUpdate(t, m, deleteConfirmation)

	assert.Contains(t, plain(m.View()), deleteReviewTitle, "the count of the earlier dry run no longer confirms")
	assert.Zero(t, conn.edits.Load())
}

func TestAnItemChangedAfterTheReviewIsKeptAndReported(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := dryRun(t, newUpdateModel(t, conn, store), deleteShipped)
	changed := json.RawMessage(`{"id":"o2","customerId":"c01","status":"shipped","total":2,"memo":"edited elsewhere"}`)
	require.NoError(t, conn.store.PutItem(ordersPath, changed))

	m = confirmUpdate(t, m, deleteConfirmation)
	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, "Results · mock · deleted")
	assert.Contains(t, view, "Deleted 3 of 4 items from sales.orders. 1 had changed and was kept.")
	assert.Contains(t, view, "skipped: changed")
	assert.Contains(t, view, "412 Precondition Fa")
	assert.Equal(t, []string{"o2", "o5"}, storedOrderIDs(t, conn))
	require.Len(t, store.entries, 1)
	entry := store.entries[0]
	assert.Equal(t, history.KindDelete, entry.Kind)
	assert.Equal(t, 3, entry.Rows)
	assert.True(t, entry.OK, "a kept item is expected, not a failure")
}

func TestADeleteRunsBehindTheStatusBarAndSaysDeleted(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)
	m, step := heldDelete(t, m, deleteConfirmation)

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, statusBar(m), "delete mock/sales.orders 0% (w)")
	m = settleNow(m, step)
	assert.Contains(t, statusBar(m), "delete done (w)")
	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('w'), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(m.View()), "Deleted 4 of 4 items from sales.orders.")
	assert.Contains(t, statusBar(m), "deleted")
}

func TestQuittingARunningDeleteWarnsInItsOwnWords(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}
	m := dryRun(t, newUpdateModel(t, conn, store), deleteShipped)
	m, _ = heldDelete(t, m, deleteConfirmation)
	m = pressAll(t, m, keyMsg(tea.KeyEscape), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab))

	m, msgs := press(t, m, keyRune('q'))

	require.False(t, hasMsg[tea.QuitMsg](msgs))
	assert.Contains(t, plain(m.View()), "A delete is running. Quit again to stop it and quit; items already deleted stay")
}

func TestRecallAndRunReviewsADeleteAgain(t *testing.T) {
	conn := newUpdateConnection(t)
	m := confirmUpdate(t, dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped), deleteConfirmation)
	m = pressAll(t, m, keyMsg(tea.KeyEnter))
	edits := conn.edits.Load()
	require.NoError(t, conn.store.PutItem(ordersPath, shippedOrder("o6", "c03", "shipped")))

	m = pressNow(t, pressAll(t, m, keyMsg(tea.KeyCtrlO)), keyMsg(tea.KeyCtrlR))

	view := plain(m.View())
	assert.Contains(t, view, deleteReviewTitle, "a rerun selects afresh and asks again")
	assert.Contains(t, view, "1 matched at")
	assert.Equal(t, edits, conn.edits.Load())
}

func TestASavedDeleteRecalledAndRunEndsInTheReview(t *testing.T) {
	store := newSavedStore(t)
	create(t, store, mock.Name, saved.Query{Name: "drop shipped", Text: deleteShipped})
	conn := newUpdateConnection(t)
	m := newModelWith(t, conn, tui.Options{Saved: store, History: &recordingStore{}, Manage: managed})
	m, _ = settle(m, m.Init())

	m = pressNow(t, openSaved(t, m), keyMsg(tea.KeyCtrlR))

	assert.Contains(t, plain(m.View()), deleteReviewTitle)
	assert.Zero(t, conn.edits.Load())
}

func TestAReadOnlyAccountRefusesADeleteBeforeReadingAnything(t *testing.T) {
	conn := newUpdateConnection(t)
	store := &recordingStore{}

	m := dryRun(t, newUpdateModel(t, conn, store, tui.Account{Name: mock.Name, ReadOnly: true}), deleteShipped)

	assert.Contains(t, plain(m.View()), "mock is read-only, so nothing was sent.")
	assert.Zero(t, conn.scans)
	assert.Zero(t, conn.edits.Load())
	require.Len(t, store.entries, 1)
	assert.Equal(t, history.KindDelete, store.entries[0].Kind)
	assert.False(t, store.entries[0].OK)
}

func TestAnAccountTurnedReadOnlyDuringTheReviewDeletesNothing(t *testing.T) {
	conn := newUpdateConnection(t)
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), deleteShipped)
	m, _ = m.Update(tui.AccountsListedMsg{Accounts: []tui.Account{{Name: mock.Name, ReadOnly: true}}})

	m = confirmUpdate(t, m, deleteConfirmation)

	assert.Contains(t, plain(m.View()), "mock is read-only, so nothing was sent.")
	assert.Zero(t, conn.edits.Load())
	assert.Len(t, storedOrderIDs(t, conn), 5)
}

func TestADeleteThatDoesNotParseSaysHow(t *testing.T) {
	tests := []struct {
		statement string
		want      string
	}{
		{statement: `DELETE FROM sales.orders o`, want: "DELETE needs a WHERE. To delete every item, write WHERE true"},
		{statement: `DELETE sales.orders o WHERE o.status = "x"`, want: "DELETE needs FROM: DELETE FROM sales.orders o WHERE …"},
		{statement: `DELETE o.tmp FROM sales.orders o WHERE true`, want: "DELETE removes whole items"},
	}
	for _, tt := range tests {
		t.Run(tt.statement, func(t *testing.T) {
			conn := newUpdateConnection(t)
			store := &recordingStore{}

			m := dryRun(t, newUpdateModel(t, conn, store), tt.statement)

			assert.Contains(t, plain(m.View()), "the delete does not parse, so nothing was read")
			assert.Contains(t, plain(m.View()), tt.want)
			assert.Zero(t, conn.scans)
			require.Len(t, store.entries, 1)
			assert.Equal(t, history.KindDelete, store.entries[0].Kind)
		})
	}
}

func TestDeletingEveryItemPointsAtTheContainer(t *testing.T) {
	conn := newUpdateConnection(t)

	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), `DELETE FROM sales.orders o WHERE true`)

	view := plain(m.View())
	assert.Contains(t, view, "Every item in sales.orders. Deleting and recreating the container")
	assert.Contains(t, view, "5 matched at")
	confirmUpdate(t, m, "orders 5")
	assert.Equal(t, int32(5), conn.edits.Load())
	assert.Empty(t, storedOrderIDs(t, conn))
}

func TestNoMatchIsNothingToDelete(t *testing.T) {
	conn := newUpdateConnection(t, mock.WithPredicate(`o.status = "lost"`, func(json.RawMessage) bool { return false }))

	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), `DELETE FROM sales.orders o WHERE o.status = "lost"`)

	assert.Contains(t, plain(m.View()), "No item in sales.orders matches. Nothing to delete.")
}

func TestARunningDeleteHoldsTheJobSlot(t *testing.T) {
	conn := newUpdateConnection(t)
	m := newWideModel(t, conn, tui.Options{Manage: managed, Snapshots: t.TempDir(),
		Accounts: []tui.Account{{Name: mock.Name, Database: firstDatabase}}})
	m = dryRun(t, m, deleteShipped)
	m, _ = heldDelete(t, m, deleteConfirmation)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))

	m = pressAll(t, m, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab), keyRune('s'))
	assert.Contains(t, statusBar(m), "a delete is running: snapshots wait for it (w)")
	m = rerun(t, pressAll(t, m, keyRune('e')), archiveShipped)
	assert.Contains(t, plain(m.View()), "a delete is running on mock/sales.orders: w in the catalog shows it")
	m = rerun(t, m, `BEGIN BATCH sales.orders PARTITION "c01"; DELETE "o1"; COMMIT`)
	assert.Contains(t, plain(m.View()), "a delete is writing sales.orders: the batch waits for it (w)")
}

func TestTheDeleteReviewFitsTheSmallestTerminal(t *testing.T) {
	conn := newUpdateConnection(t)
	m := newModelWith(t, conn, tui.Options{Manage: managed})
	m, _ = settle(m, m.Init())

	m = dryRun(t, m, deleteShipped)

	view := plain(m.View())
	assert.Contains(t, view, "Type orders 4 to delete:")
	assert.Contains(t, view, "> ", "the confirmation stays in view")
	assert.Contains(t, view, "enter start")
}

func TestAThousandsCountIsTypedInPlainDigits(t *testing.T) {
	items := make([]json.RawMessage, 1204)
	for i := range items {
		items[i] = shippedOrder(fmt.Sprintf("o%04d", i), fmt.Sprintf("c%02d", i%7), "shipped")
	}
	conn := newConnection(t, mock.WithPredicate("true", func(json.RawMessage) bool { return true }), mock.WithItems(ordersPath, items...))
	m := dryRun(t, newUpdateModel(t, conn, &recordingStore{}), `DELETE FROM sales.orders o WHERE true`)
	require.Contains(t, plain(m.View()), "1,204 matched at")
	require.Contains(t, plain(m.View()), "Type orders 1204 to delete:")

	m = confirmUpdate(t, m, "orders 1,204")
	assert.Contains(t, plain(m.View()), deleteReviewTitle, "the count as the review shows it does not confirm")
	assert.Zero(t, conn.edits.Load())

	m = pressAll(t, m, keyMsg(tea.KeyCtrlU))
	heldDelete(t, m, "orders 1204")
}

func TestADeleteThatEndedBehindItsHiddenViewLetsTheNextOneStart(t *testing.T) {
	tests := []struct {
		name     string
		keys     []tea.KeyMsg
		label    string
		recorded bool
	}{
		{name: "done", keys: []tea.KeyMsg{keyMsg(tea.KeyEscape)}, label: "delete done (w)", recorded: true},
		{name: "stopped", keys: []tea.KeyMsg{keyRune('x'), keyMsg(tea.KeyEscape)}, label: "delete stopped (w)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newUpdateConnection(t)
			store := &recordingStore{}
			m := dryRun(t, newUpdateModel(t, conn, store), deleteShipped)
			m, step := heldDelete(t, m, deleteConfirmation)
			m = settleNow(pressAll(t, m, tt.keys...), step)
			require.Contains(t, statusBar(m), tt.label, "the ended job's view, and its report, are a w away")

			m = rerun(t, m, `DELETE FROM sales.orders o WHERE true`)
			require.Contains(t, plain(m.View()), deleteReviewTitle, "an ended job keeps no other job out")
			assert.Empty(t, store.entries, "the ended job is recorded once the next one replaces it")

			left := len(storedOrderIDs(t, conn))
			m = confirmUpdate(t, m, fmt.Sprintf("orders %d", left))
			require.Len(t, store.entries, 1, "the job the new one replaced is recorded")
			assert.Equal(t, deleteShipped, store.entries[0].Query)
			assert.Equal(t, history.KindDelete, store.entries[0].Kind)
			assert.Equal(t, tt.recorded, store.entries[0].OK)
			m = pressAll(t, m, keyMsg(tea.KeyEnter))
			assert.Contains(t, plain(m.View()), fmt.Sprintf("Deleted %d of %d item", left, left))
			assert.Empty(t, storedOrderIDs(t, conn))
		})
	}
}
