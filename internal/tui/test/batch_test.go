package tui_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The review's title as it appears inside the top border.
const reviewTitle = " Review batch "

// writingBatch creates one order and deletes another in partition c01 of
// sales.orders, which the seeded store holds.
const writingBatch = `BEGIN BATCH sales.orders PARTITION "c01"; CREATE {"id": "o9", "customerId": "c01"}; DELETE "o1"; COMMIT`

var ordersPath = []string{firstDatabase, firstContainer}

func order(id string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"customerId":"c01","status":"open"}`, id))
}

// newOrdersConnection serves a mock whose sales.orders holds o1 to o5, all
// in partition c01.
func newOrdersConnection(t *testing.T, opts ...mock.Option) *recordingConnection {
	t.Helper()
	seed := mock.WithItems(ordersPath, order("o1"), order("o2"), order("o3"), order("o4"), order("o5"))
	return newConnection(t, append([]mock.Option{seed}, opts...)...)
}

func newBatchModel(t *testing.T, conn adapter.Connection, store history.Store) tea.Model {
	t.Helper()
	m := newModelWith(t, conn, tui.Options{Manage: managed, History: store})
	model, _ := settle(m, m.Init())
	return model
}

func openReview(t *testing.T, m tea.Model, batch string) tea.Model {
	t.Helper()
	return runQuery(t, m, batch)
}

func commit(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyText(firstContainer), keyMsg(tea.KeyEnter))
}

func storedIDs(t *testing.T, conn *recordingConnection) []string {
	t.Helper()
	var ids []string
	for _, item := range conn.store.Items(ordersPath) {
		var head struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(item, &head))
		ids = append(ids, head.ID)
	}
	return ids
}

func TestCtrlROnABatchOpensTheReviewAndSendsNothing(t *testing.T) {
	conn := newOrdersConnection(t)

	m := openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch)

	view := plain(m.View())
	assert.Contains(t, view, reviewTitle)
	for _, want := range []string{"mock", "sales.orders", `/customerId = "c01"`, "2 (1 create, 1 delete)", "CREATE   o9", "DELETE   o1",
		"DELETE o1 has no IF MATCH", "Type the container name to commit:"} {
		assert.Contains(t, view, want)
	}
	assert.Empty(t, conn.batches)
}

func TestOnlyTheExactNameCommits(t *testing.T) {
	for _, typed := range []string{"", "Orders", "orders "} {
		t.Run(fmt.Sprintf("%q", typed), func(t *testing.T) {
			conn := newOrdersConnection(t)
			m := openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch)

			m = pressAll(t, m, keyText(typed), keyMsg(tea.KeyEnter))

			assert.Contains(t, plain(m.View()), reviewTitle)
			assert.Empty(t, conn.batches)
		})
	}
	t.Run("the name", func(t *testing.T) {
		conn := newOrdersConnection(t)

		commit(t, openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch))

		assert.Len(t, conn.batches, 1)
	})
}

func TestEscClosesTheReviewHavingSentAndRecordedNothing(t *testing.T) {
	conn := newOrdersConnection(t)
	store := &recordingStore{}

	m := pressAll(t, openReview(t, newBatchModel(t, conn, store), writingBatch), keyMsg(tea.KeyEscape))

	view := plain(m.View())
	assert.NotContains(t, view, reviewTitle)
	assert.Contains(t, view, `BEGIN BATCH sales.orders`, "the buffer is left as it was")
	assert.Empty(t, conn.batches)
	assert.Empty(t, store.entries)
}

func TestTypedLettersInTheReviewAreTheName(t *testing.T) {
	m := openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch)

	m, msgs := press(t, m, keyRune('q'))

	assert.False(t, hasMsg[tea.QuitMsg](msgs))
	assert.Contains(t, plain(m.View()), "> q")
}

func TestCtrlGInTheReviewOpensNoSwitcher(t *testing.T) {
	m := openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch)

	m = pressAll(t, m, keyMsg(tea.KeyCtrlG))

	view := plain(m.View())
	assert.Contains(t, view, reviewTitle)
	assert.NotContains(t, view, accountsTitle)
}

func TestACommittedBatchIsReportedAsAPage(t *testing.T) {
	conn := newOrdersConnection(t)
	store := &recordingStore{}

	m := commit(t, openReview(t, newBatchModel(t, conn, store), writingBatch))

	view := plain(m.View())
	assert.Contains(t, view, "Results · mock · committed")
	assert.Contains(t, view, "Committed: 2 operations")
	assert.Contains(t, statusBar(m), "committed ▪ 2 operations ▪ 14.00 RU")
	assert.Equal(t, []string{"o2", "o3", "o4", "o5", "o9"}, storedIDs(t, conn))
	require.Len(t, store.entries, 1)
	entry := store.entries[0]
	assert.Equal(t, history.KindBatch, entry.Kind)
	assert.True(t, entry.OK)
	assert.Equal(t, 2, entry.Rows)
	assert.Equal(t, []string{firstDatabase, firstContainer}, entry.Scope)
	assert.Equal(t, writingBatch, entry.Query)
}

func TestAReadRowOpensItsDocument(t *testing.T) {
	batch := `BEGIN BATCH sales.orders PARTITION "c01"; DELETE "o1"; READ "o2"; COMMIT`
	m := commit(t, openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), batch))

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))

	view := plain(m.View())
	assert.Contains(t, view, detailTitle)
	assert.Contains(t, view, `"status": "open"`)
}

func TestTheReportExportsLikeAnyResultSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	m := commit(t, openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch))

	exportTo(t, focusResults(t, m), path)

	documents := exportedDocuments(t, path)
	require.Len(t, documents, 2)
	assert.Contains(t, string(documents[0]), `"operation": "CREATE"`)
}

func TestARollbackShowsTheFailedOperationAndWritesNothing(t *testing.T) {
	conn := newOrdersConnection(t, mock.WithBatchFailure(3, http.StatusNotFound))
	store := &recordingStore{}
	batch := `BEGIN BATCH sales.orders PARTITION "c01"; DELETE "o1"; DELETE "o2"; DELETE "o3"; DELETE "o4"; DELETE "o5"; COMMIT`
	before := storedIDs(t, conn)

	m := commit(t, openReview(t, newBatchModel(t, conn, store), batch))

	view := plain(m.View())
	assert.Contains(t, view, "Results · mock · rolled back")
	assert.Contains(t, view, "Rolled back: nothing was written.")
	assert.Contains(t, statusBar(m), "rolled back")
	assert.Equal(t, before, storedIDs(t, conn))
	require.Len(t, store.entries, 1)
	assert.False(t, store.entries[0].OK)
	assert.Equal(t, `rolled back: operation 3 DELETE "o3": 404 Not Found`, store.entries[0].Error)

	detail := plain(pressAll(t, focusResults(t, m), keyMsg(tea.KeyEnter)).View())
	assert.Contains(t, detail, `"outcome": "failed"`, "the cursor opens on the failed row")
}

func TestAValidationFailureListsEveryProblemAndIsRecorded(t *testing.T) {
	conn := newOrdersConnection(t)
	store := &recordingStore{}
	batch := `BEGIN BATCH sales.orders PARTITION "c01"; CREATE {"id": "o9", "customerId": "c02"}; REPLACE "o2" {"customerId": "c01"}; COMMIT`

	m := runQuery(t, newBatchModel(t, conn, store), batch)

	view := plain(m.View())
	assert.Contains(t, view, "Batch refused, nothing was sent:")
	assert.Contains(t, view, `the batch is for "c01"`)
	assert.Contains(t, view, `body has no "id"`)
	assert.NotContains(t, view, reviewTitle)
	assert.Empty(t, conn.batches)
	require.Len(t, store.entries, 1)
	assert.Equal(t, history.KindBatch, store.entries[0].Kind)
	assert.Contains(t, store.entries[0].Error, "Batch refused")
}

func TestASyntaxErrorIsRefusedAndRecorded(t *testing.T) {
	store := &recordingStore{}

	m := runQuery(t, newBatchModel(t, newOrdersConnection(t), store), `BEGIN BATCH sales.orders PARTITION "c01"; READ "o1";`)

	assert.Contains(t, plain(m.View()), "no COMMIT")
	require.Len(t, store.entries, 1)
	assert.Equal(t, history.KindBatch, store.entries[0].Kind)
}

func TestATargetWhoseDatabaseIsNotListedYetWaitsForIt(t *testing.T) {
	conn := newOrdersConnection(t)
	m, prefetch := rootOnly(t, conn)
	m = runQuery(t, m, writingBatch)
	require.NotContains(t, plain(m.View()), reviewTitle)

	m, _ = settle(m, prefetch)

	assert.Contains(t, plain(m.View()), reviewTitle)
	assert.Equal(t, 1, conn.calls[firstDatabase])
}

func TestAnUnknownContainerIsRefused(t *testing.T) {
	conn := newOrdersConnection(t)

	m := runQuery(t, newBatchModel(t, conn, &recordingStore{}), `BEGIN BATCH sales.invoices PARTITION "c01"; DELETE "o1"; COMMIT`)

	assert.Contains(t, plain(m.View()), "sales.invoices: no such container")
	assert.Empty(t, conn.batches)
}

func newReadOnlyModel(t *testing.T, conn adapter.Connection, store history.Store) tea.Model {
	t.Helper()
	m := newModelWith(t, conn, tui.Options{
		Accounts: []tui.Account{{Name: mock.Name, ReadOnly: true}},
		Manage:   managed,
		History:  store,
	})
	model, _ := settle(m, m.Init())
	return model
}

func TestAReadOnlyAccountRefusesAWrite(t *testing.T) {
	conn := newOrdersConnection(t)
	store := &recordingStore{}

	m := runQuery(t, newReadOnlyModel(t, conn, store), writingBatch)

	view := plain(m.View())
	assert.Contains(t, view, "mock is read-only, so nothing was sent.")
	assert.Contains(t, view, "alchemist profile set-read-only mock false")
	assert.Empty(t, conn.batches)
	require.Len(t, store.entries, 1)
	assert.Contains(t, statusBar(m), "mock ▪ read-only")
}

func TestAReadOnlyAccountRunsABatchThatOnlyReads(t *testing.T) {
	conn := newOrdersConnection(t)

	m := runQuery(t, newReadOnlyModel(t, conn, &recordingStore{}), `BEGIN BATCH sales.orders PARTITION "c01"; READ "o1"; COMMIT`)

	assert.NotContains(t, plain(m.View()), reviewTitle, "a batch that writes nothing needs no review")
	assert.Len(t, conn.batches, 1)
	assert.Contains(t, plain(m.View()), "Committed: 1 operation")
}

func TestTheSessionFlagMakesEveryAccountReadOnly(t *testing.T) {
	conn := newOrdersConnection(t)
	m := newModelWith(t, conn, tui.Options{Manage: managed, ReadOnly: true})
	m, _ = settle(m, m.Init())

	m = runQuery(t, m, writingBatch)

	assert.Contains(t, plain(m.View()), "mock is read-only")
	assert.Empty(t, conn.batches)
}

func TestAConnectionWithNoBatcherRefuses(t *testing.T) {
	conn := newOrdersConnection(t)
	m := newModelWith(t, conn, tui.Options{Manage: func(adapter.Connection) tui.Management { return tui.Management{} }})
	m, _ = settle(m, m.Init())

	m = runQuery(t, m, writingBatch)

	assert.Contains(t, plain(m.View()), "this adapter has no transactions")
}

func TestAnUnknownOutcomeSaysHowToCheck(t *testing.T) {
	conn := newOrdersConnection(t, mock.WithError(mock.OpBatchUnknown))
	store := &recordingStore{}

	m := commit(t, openReview(t, newBatchModel(t, conn, store), writingBatch))

	view := plain(m.View())
	assert.Contains(t, view, "Outcome unknown. The batch was sent and no answer")
	assert.Contains(t, view, "not retry it and will not.")
	assert.Contains(t, view, "Check before running it again:")
	assert.Contains(t, view, "c.customerId")
	assert.NotContains(t, view, "Committed")
	assert.NotContains(t, view, "Not applied")
	assert.Contains(t, statusBar(m), "outcome unknown")
	assert.Len(t, conn.batches, 1)
	require.Len(t, store.entries, 1)
	assert.True(t, strings.HasPrefix(store.entries[0].Error, "outcome unknown: "), store.entries[0].Error)
}

func TestARefusedBatchReadsNotApplied(t *testing.T) {
	conn := newOrdersConnection(t, mock.WithError(mock.OpBatch))

	m := commit(t, openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch))

	assert.Contains(t, plain(m.View()), "Not applied: mock: injected batch error")
	assert.Len(t, conn.batches, 1)
}

// throttlingBatcher refuses every batch for rate, naming a wait.
type throttlingBatcher struct{}

func (throttlingBatcher) ExecuteBatch(context.Context, adapter.Batch) (adapter.BatchResult, error) {
	return adapter.BatchResult{}, &adapter.ThrottledError{RetryAfter: 1200 * time.Millisecond, Err: errors.New("429 Too Many Requests")}
}

func TestAThrottledBatchPassesOnTheWaitAndIsNotRetried(t *testing.T) {
	conn := newOrdersConnection(t)
	conn.batcher = throttlingBatcher{}

	m := commit(t, openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch))

	view := plain(m.View())
	assert.Contains(t, view, "Not applied: 429 Too Many Requests (retry after")
	assert.Contains(t, view, "1.2s)")
	assert.Len(t, conn.batches, 1)
}

func TestWhileABatchCommitsTheSessionWaitsForItsOutcome(t *testing.T) {
	conn := newOrdersConnection(t)
	m := pressAll(t, openReview(t, newBatchModel(t, conn, &recordingStore{}), writingBatch), keyText(firstContainer))
	m, sent := m.Update(keyMsg(tea.KeyEnter)) // the send is held back until the test delivers it

	assert.Contains(t, statusBar(m), "committing…")
	for _, k := range []tea.KeyMsg{keyMsg(tea.KeyCtrlR), keyMsg(tea.KeyEscape), keyMsg(tea.KeyCtrlG)} {
		var msgs []tea.Msg
		m, msgs = press(t, m, k)
		assert.False(t, hasMsg[tea.QuitMsg](msgs))
	}
	m, msgs := press(t, focusResults(t, m), keyRune('q'))
	assert.False(t, hasMsg[tea.QuitMsg](msgs), "q does not quit")
	assert.NotContains(t, plain(m.View()), accountsTitle)
	assert.Contains(t, statusBar(m), "committing")
	assert.Empty(t, conn.batches)

	m, _ = settle(m, sent)

	assert.Len(t, conn.batches, 1)
	assert.Contains(t, plain(m.View()), "Committed: 2 operations")
}

func TestQTypedInTheEditorWhileABatchCommitsIsText(t *testing.T) {
	m := pressAll(t, openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch), keyText(firstContainer))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	m, msgs := press(t, m, keyRune('q'))

	assert.False(t, hasMsg[tea.QuitMsg](msgs))
	assert.Contains(t, plain(m.View()), "COMMITq")
}

func TestCtrlCQuitsWhileABatchCommits(t *testing.T) {
	m := pressAll(t, openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch), keyText(firstContainer))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	_, msgs := press(t, m, keyMsg(tea.KeyCtrlC))

	assert.True(t, hasMsg[tea.QuitMsg](msgs))
}

func TestABatchRecalledFromHistoryIsReviewedAgain(t *testing.T) {
	conn := newOrdersConnection(t)
	store := &recordingStore{}
	m := commit(t, openReview(t, newBatchModel(t, conn, store), writingBatch))
	require.Len(t, conn.batches, 1)

	m = pressAll(t, openHistory(t, m), keyMsg(tea.KeyCtrlR))

	assert.Contains(t, plain(m.View()), reviewTitle, "a confirmation is never remembered")
	assert.Len(t, conn.batches, 1)
	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, plain(m.View()), "BEGIN BATCH sales.orders")
}

func TestTheHistoryTagsABatch(t *testing.T) {
	store := &recordingStore{}
	m := commit(t, openReview(t, newBatchModel(t, newOrdersConnection(t), store), writingBatch))

	assert.Contains(t, plain(openHistory(t, m).View()), "batch sales.orders")
}

func TestRecallingABatchLeavesTheScopeAlone(t *testing.T) {
	store := &recordingStore{}
	m := commit(t, openReview(t, newBatchModel(t, newOrdersConnection(t), store), writingBatch))

	m = pressAll(t, openHistory(t, m), keyMsg(tea.KeyEnter))

	assert.Contains(t, statusBar(m), "no scope")
}

func TestAnOversizeBatchIsRecordedWithoutItsCommit(t *testing.T) {
	store := &recordingStore{}
	var reads strings.Builder
	for i := range 70 {
		fmt.Fprintf(&reads, ` READ "%04d%s";`, i, strings.Repeat("x", 1000))
	}
	batch := `BEGIN BATCH sales.orders PARTITION "c01";` + reads.String() + ` COMMIT`

	runQuery(t, newBatchModel(t, newOrdersConnection(t), store), batch)

	require.Len(t, store.entries, 1)
	recorded := store.entries[0].Query
	assert.LessOrEqual(t, len(recorded), 64*1024)
	assert.NotContains(t, recorded, "COMMIT")
	_, err := query.ParseBatch(recorded)
	require.Error(t, err)
}

func TestSavingABatchSavesNoScope(t *testing.T) {
	for _, batch := range []string{writingBatch, strings.TrimSuffix(writingBatch, " COMMIT")} {
		store := newSavedStore(t)
		m := newSavedModel(t, newOrdersConnection(t), store)

		saveAs(t, typeQuery(t, m, batch), "cleanup")

		assert.Empty(t, only(t, store, mock.Name).Scope, batch)
	}
}

func TestCtrlRInTheSavedOverlayOpensTheReview(t *testing.T) {
	conn := newOrdersConnection(t)
	store := newSavedStore(t)
	m := newModelWith(t, conn, tui.Options{Manage: managed, Saved: store})
	m, _ = settle(m, m.Init())
	m = saveAs(t, typeQuery(t, m, writingBatch), "cleanup")

	m = pressAll(t, openSaved(t, m), keyMsg(tea.KeyCtrlR))

	assert.Contains(t, plain(m.View()), reviewTitle)
	assert.Empty(t, conn.batches)
}

func TestWithNoAccountABatchIsRefused(t *testing.T) {
	o := newOpener(t)
	m := pressAll(t, newAccountsModel(t, o, tui.Options{}), keyMsg(tea.KeyCtrlG), keyRune('x'), keyMsg(tea.KeyEscape))

	m = runQuery(t, m, writingBatch)

	assert.Contains(t, plain(m.View()), "no account connected")
	assert.NotContains(t, plain(m.View()), reviewTitle)
}

func TestABatchWritesOnlyToTheAccountItRanOn(t *testing.T) {
	o := newOpener(t)
	store := &recordingStore{}
	m := newAccountsModel(t, o, tui.Options{History: store})
	m = selectContainer(t, m)

	m = pressAll(t, runQuery(t, m, `BEGIN BATCH sales.orders PARTITION "c01"; CREATE {"id": "o9", "customerId": "c01"}; COMMIT`),
		keyText(firstContainer), keyMsg(tea.KeyEnter))
	require.Len(t, o.last("prod").batches, 1)
	m = switchTo(t, m, "staging")

	assert.Len(t, o.last("prod").store.Items(ordersPath), 1)
	assert.Empty(t, o.last("staging").store.Items(ordersPath))
	assert.Empty(t, o.last("staging").batches)
	require.Len(t, store.entries, 1)
	assert.Equal(t, "prod", store.entries[0].Profile)
	assert.NotContains(t, plain(openHistory(t, m).View()), "batch sales.orders")
}

func TestTheSameBatchOnAReadOnlyAccountIsRefusedThere(t *testing.T) {
	o := newOpener(t)
	accounts := fixtureAccounts()
	accounts[2].ReadOnly = true // staging
	m := newModelWithAccounts(t, o, accounts)
	m = typeQuery(t, m, writingBatch)

	m = pressAll(t, switchTo(t, m, "staging"), keyMsg(tea.KeyCtrlR))
	assert.Contains(t, plain(m.View()), "staging is read-only")

	m = pressAll(t, switchTo(t, m, "prod"), keyMsg(tea.KeyCtrlR))
	assert.Contains(t, plain(m.View()), reviewTitle, "prod can still write")
}

func newModelWithAccounts(t *testing.T, o *opener, accounts []tui.Account) tea.Model {
	t.Helper()
	m, _ := tui.New(tui.Options{
		Icons:    theme.Icons(),
		Accounts: accounts,
		Launch:   "prod",
		Open:     o.open,
		Manage:   managed,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m, _ = settle(m, m.Init())
	return m
}

// newLedgerConnection adds sales.ledger, keyed on the pk field every canned
// row carries, so a row on screen can be drafted into a batch.
func newLedgerConnection(t *testing.T) *recordingConnection {
	t.Helper()
	conn := newConnection(t)
	require.NoError(t, conn.admin.CreateContainer(context.Background(),
		adapter.ContainerSpec{Database: firstDatabase, Name: "ledger", PartitionKeys: []string{"/pk"}}))
	return conn
}

func TestCtrlBDraftsAReplaceThatReviews(t *testing.T) {
	m := runQuery(t, newBatchModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT * FROM sales.ledger c")

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyCtrlB), keyMsg(tea.KeyCtrlR))

	view := plain(m.View())
	assert.Contains(t, view, reviewTitle)
	assert.Contains(t, view, `/pk = "pk-0"`)
	assert.Contains(t, view, "REPLACE  item-1-0")
}

func TestCtrlBAppendsToTheBatchInTheEditor(t *testing.T) {
	m := runQuery(t, newBatchModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT * FROM sales.ledger c")
	m = focusResults(t, m)

	m = pressAll(t, m, keyMsg(tea.KeyCtrlB), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyCtrlB))
	m = pressAll(t, m, keyMsg(tea.KeyCtrlR))

	view := plain(m.View())
	assert.Contains(t, view, "2 (2 replace)", "rows 0 and 3 share partition pk-0")
	assert.Contains(t, view, "REPLACE  item-1-3")
}

func TestCtrlBRefusesARowOfAnotherPartition(t *testing.T) {
	m := runQuery(t, newBatchModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT * FROM sales.ledger c")

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyCtrlB), keyMsg(tea.KeyDown), keyMsg(tea.KeyCtrlB), keyMsg(tea.KeyCtrlR))

	assert.Contains(t, plain(m.View()), "1 (1 replace)")
}

func TestCtrlBRefusesAProjectedRow(t *testing.T) {
	m := runQuery(t, newBatchModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT c.id, c.pk FROM sales.ledger c")

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyCtrlB))

	assert.NotContains(t, plain(m.View()), "BEGIN BATCH", "a replace from a projection would erase every field it left out")
}

func TestCtrlBRefusesASimulatedRow(t *testing.T) {
	m := runQuery(t, newBatchModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT * FROM sales.ledger, sales.orders")

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyCtrlB))

	assert.NotContains(t, plain(m.View()), "BEGIN BATCH")
}

func TestCtrlBIsAbsentOnAReadOnlyAccount(t *testing.T) {
	m := runQuery(t, newReadOnlyModel(t, newLedgerConnection(t), &recordingStore{}), "SELECT * FROM sales.ledger c")

	m = pressAll(t, focusResults(t, m), keyMsg(tea.KeyCtrlB))

	assert.NotContains(t, plain(m.View()), "BEGIN BATCH")
	assert.NotContains(t, plain(pressAll(t, m, keyRune('?')).View()), "add to batch")
}

func TestAQueryStillRunsWithNoReview(t *testing.T) {
	conn := newOrdersConnection(t)

	m := runQuery(t, selectContainer(t, newBatchModel(t, conn, &recordingStore{})), "SELECT * FROM c")

	assert.NotContains(t, plain(m.View()), reviewTitle)
	assert.Len(t, conn.queries, 1)
}

func TestTheReviewScrollsAndKeepsTheNameFieldInView(t *testing.T) {
	var ops strings.Builder
	for i := range 40 {
		fmt.Fprintf(&ops, ` DELETE "d%02d" IF MATCH "e";`, i)
	}
	m := openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}),
		`BEGIN BATCH sales.orders PARTITION "c01";`+ops.String()+` COMMIT`)
	require.Contains(t, plain(m.View()), "d00")

	for range 30 {
		m = pressAll(t, m, keyMsg(tea.KeyDown))
	}

	view := plain(m.View())
	assert.NotContains(t, view, "d00")
	assert.Contains(t, view, "d39")
	assert.Contains(t, view, "Type the container name to commit:")
}

// pendingOnRootOnly starts a batch on a model whose sales listing is still
// out, and returns that listing's command to deliver later.
func pendingOnRootOnly(t *testing.T, conn *recordingConnection, batch string) (tea.Model, tea.Cmd) {
	t.Helper()
	m, prefetch := rootOnly(t, conn)
	m = runQuery(t, m, batch)
	require.NotContains(t, plain(m.View()), reviewTitle)
	return m, prefetch
}

func TestAQueryStartedAfterAWaitingBatchIsNotReplaced(t *testing.T) {
	for _, batch := range []string{writingBatch, `BEGIN BATCH sales.orders PARTITION "c01"; READ "o1"; COMMIT`} {
		conn := newOrdersConnection(t)
		m, prefetch := pendingOnRootOnly(t, conn, batch)

		m = runAnother(t, m, "SELECT * FROM sales.orders c")
		m, _ = settle(m, prefetch)

		view := plain(m.View())
		assert.NotContains(t, view, reviewTitle, batch)
		assert.Empty(t, conn.batches, batch)
		assert.Contains(t, view, "item-1-0", "the query's rows stay on screen")
	}
}

func TestSwitchingAccountsDropsAWaitingBatch(t *testing.T) {
	o := newOpener(t)
	m := newAccountsModel(t, o, tui.Options{})
	m, relisting := m.Update(keyRune('r')) // sales is listed again, and the answer held back
	m = runQuery(t, m, writingBatch)
	require.NotContains(t, plain(m.View()), reviewTitle, "the batch waits for the listing")

	m = switchTo(t, m, "staging")
	m, _ = settle(m, relisting)

	assert.NotContains(t, plain(m.View()), reviewTitle)
	assert.Empty(t, o.last("prod").batches)
}

func TestTheConnectFormNeverLoosensReadOnlyOnANewEndpoint(t *testing.T) {
	o := newOpener(t)
	o.failOpen["emulator"] = tui.ErrCredentialsNeeded
	c := &connector{t: t}
	m := switchTo(t, newAccountsModel(t, o, tui.Options{Connect: c.connect}), "emulator")
	require.Contains(t, plain(m.View()), "Profile emulator has no key")

	m = pressAll(t, m, keyMsg(tea.KeyShiftTab), keyMsg(tea.KeyCtrlU), keyText("https://prod.documents.azure.com:443/"),
		keyMsg(tea.KeyTab), keyText("typed-key"), keyMsg(tea.KeyEnter))

	onAccount(t, m, "emulator")
	require.Len(t, c.forms, 1)
	require.Equal(t, "https://prod.documents.azure.com:443/", c.forms[0].Endpoint)
	assert.Contains(t, statusBar(m), "emulator ▪ read-only")
}

// completeStaging connects staging, which has no key, through the form with
// its endpoint as the fixture lists it.
func completeStaging(t *testing.T, c *connector, opts tui.Options) tea.Model {
	t.Helper()
	o := newOpener(t)
	o.failOpen["staging"] = tui.ErrCredentialsNeeded
	opts.Connect = c.connect
	m := switchTo(t, newAccountsModel(t, o, opts), "staging")
	m = pressAll(t, m, keyText("typed-key"), keyMsg(tea.KeyEnter))
	onAccount(t, m, "staging")
	return m
}

func TestAFormConnectKeepsWhatTheSavedProfileAllows(t *testing.T) {
	writable := false
	c := &connector{t: t, readOnly: &writable}
	staging := tui.Account{Name: "staging", Endpoint: "https://staging.documents.azure.com:443/"}

	m := completeStaging(t, c, tui.Options{ListAccounts: func() ([]tui.Account, error) { return []tui.Account{staging}, nil }})

	assert.NotContains(t, statusBar(m), "read-only", "read_only = false holds after the form")
	m = pressAll(t, m, keyMsg(tea.KeyCtrlG), keyMsg(tea.KeyEscape))
	assert.NotContains(t, statusBar(m), "read-only", "and after the profiles are read again")
	m = runQuery(t, m, `BEGIN BATCH sales.orders PARTITION "c01"; CREATE {"id": "o9", "customerId": "c01"}; COMMIT`)
	assert.Contains(t, plain(m.View()), reviewTitle)
}

func TestTheSessionFlagStillAppliesToAFormConnect(t *testing.T) {
	writable := false

	m := completeStaging(t, &connector{t: t, readOnly: &writable}, tui.Options{ReadOnly: true})

	assert.Contains(t, statusBar(m), "staging ▪ read-only")
}
