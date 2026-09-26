package tui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The overlay title as it appears inside the top border.
const historyTitle = " History "

// recordingStore is an in-memory history.Store a test can read back, and
// can tell to refuse a write or a read.
type recordingStore struct {
	entries    []history.Entry
	failAppend error
	failRecent error
}

func (s *recordingStore) Append(entry history.Entry) error {
	if s.failAppend != nil {
		return s.failAppend
	}
	s.entries = append(s.entries, entry)
	return nil
}

func (s *recordingStore) Recent(account string, n int) ([]history.Entry, error) {
	if s.failRecent != nil {
		return nil, s.failRecent
	}
	var recent []history.Entry
	for i := len(s.entries) - 1; i >= 0 && len(recent) < n; i-- {
		if s.entries[i].Profile == account {
			recent = append(recent, s.entries[i])
		}
	}
	return recent, nil
}

// pastRuns is a log two sessions left behind, oldest first as the store
// keeps it.
func pastRuns() *recordingStore {
	return &recordingStore{entries: []history.Entry{
		{
			Time:    time.Now().Add(-time.Hour),
			Profile: mock.Name,
			Scope:   []string{"sales", "orders"},
			Query:   "SELECT * FROM c WHERE c.amount > 100",
			OK:      true,
			Rows:    10,
		},
		{
			Time:    time.Now().Add(-time.Minute),
			Profile: mock.Name,
			Scope:   []string{"telemetry", "events"},
			Query:   "SELECT TOP 5 * FRM c",
			Error:   "syntax error",
		},
	}}
}

// newHistoryModel is newLoadedModel with a store the test can read back.
func newHistoryModel(t *testing.T, conn adapter.Connection, store history.Store) tea.Model {
	t.Helper()
	m := newModelWith(t, conn, tui.Options{History: store})
	model, _ := settle(m, m.Init())
	return model
}

func openHistory(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyCtrlO))
}

// runAnother replaces the one-line query a run left in the editor, where
// the focus still is, and runs text instead.
func runAnother(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyCtrlU), keyText(text), keyMsg(tea.KeyCtrlR))
}

func TestARunThatProducesAPageIsRecorded(t *testing.T) {
	store := &recordingStore{}
	m := selectContainer(t, newHistoryModel(t, newConnection(t), store))

	runQuery(t, m, "SELECT * FROM c")

	require.Len(t, store.entries, 1)
	entry := store.entries[0]
	assert.True(t, entry.OK)
	assert.Equal(t, mock.Name, entry.Profile)
	assert.Equal(t, []string{"sales", "orders"}, entry.Scope)
	assert.Equal(t, "SELECT * FROM c", entry.Query)
	assert.Equal(t, 10, entry.Rows)
	assert.InDelta(t, 2.5, entry.RequestCharge, 0)
	assert.Equal(t, int64(5), entry.ElapsedMillis)
	assert.WithinDuration(t, time.Now(), entry.Time, time.Minute)
	assert.Empty(t, entry.Error)
}

func TestAFailedRunIsRecordedWithItsError(t *testing.T) {
	store := &recordingStore{}
	conn := newConnection(t)
	conn.failQuery = 1

	runQuery(t, selectContainer(t, newHistoryModel(t, conn, store)), "SELECT * FROM c")

	require.Len(t, store.entries, 1)
	assert.False(t, store.entries[0].OK)
	assert.Equal(t, "container unreachable", store.entries[0].Error)
	assert.Equal(t, "SELECT * FROM c", store.entries[0].Query)
}

func TestASupersededRunIsNotRecorded(t *testing.T) {
	store := &recordingStore{}
	m := typeQuery(t, selectContainer(t, newHistoryModel(t, newConnection(t), store)), "SELECT * FROM c")

	first, firstCmd := m.Update(keyMsg(tea.KeyCtrlR))
	second, secondCmd := first.Update(keyMsg(tea.KeyCtrlR))
	superseded, _ := settle(second, firstCmd)
	settle(superseded, secondCmd)

	assert.Len(t, store.entries, 1, "only the run whose page was shown is recorded")
}

func TestAQueryRefusedBeforeItReachesTheAdapterIsStillRecorded(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantError string
	}{
		{name: "no scope", query: "SELECT * FROM c", wantError: "no container in scope"},
		{
			name:      "unsupported shape",
			query:     "SELECT * FROM sales.orders NATURAL JOIN sales.customers",
			wantError: "NATURAL JOIN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingStore{}
			conn := newConnection(t)

			runQuery(t, newHistoryModel(t, conn, store), tt.query)

			require.Len(t, store.entries, 1)
			assert.False(t, store.entries[0].OK)
			assert.Equal(t, tt.query, store.entries[0].Query)
			assert.Contains(t, store.entries[0].Error, tt.wantError)
			assert.Empty(t, conn.queries)
		})
	}
}

func TestRunningAnEmptyBufferRecordsNothing(t *testing.T) {
	store := &recordingStore{}

	pressAll(t, newHistoryModel(t, newConnection(t), store), keyMsg(tea.KeyCtrlR))

	assert.Empty(t, store.entries)
}

func TestTheRecordedQueryIsWhatWasTypedNotWhatWasSent(t *testing.T) {
	store := &recordingStore{}

	runQuery(t, newHistoryModel(t, newConnection(t), store), "SELECT * FROM telemetry.events AS e")

	require.Len(t, store.entries, 1)
	assert.Equal(t, "SELECT * FROM telemetry.events AS e", store.entries[0].Query,
		"recall has to restore the source as it was written")
	assert.Equal(t, []string{"telemetry", "events"}, store.entries[0].Scope)
}

func TestAStoreThatRefusesTheWriteDoesNotDisturbTheRun(t *testing.T) {
	store := &recordingStore{failAppend: errors.New("disk full")}

	m := runQuery(t, selectContainer(t, newHistoryModel(t, newConnection(t), store)), "SELECT * FROM c")

	assert.Contains(t, m.View(), "item-1-0", "the page renders regardless")
	assert.NotContains(t, m.View(), "disk full")
}

func TestCtrlOListsTheRunsOfThisSessionNewestFirst(t *testing.T) {
	conn := newConnection(t)
	conn.failQuery = 1
	m := selectContainer(t, newHistoryModel(t, conn, &recordingStore{}))
	m = runQuery(t, m, "SELECT 1")
	m = runAnother(t, m, "SELECT 2")

	view := plain(openHistory(t, m).View())

	assert.Contains(t, view, historyTitle, "the overlay replaces the layout")
	assert.NotContains(t, view, catalogTitle)
	assert.Less(t, strings.Index(view, "SELECT 2"), strings.Index(view, "SELECT 1"))
	assert.Contains(t, view, theme.Icons().Failure, "the run the adapter refused is listed as failed")
	assert.Contains(t, view, theme.Icons().Success)
}

func TestHistoryOpensFromTheEditorAndEscapeCloses(t *testing.T) {
	editor := pressAll(t, newHistoryModel(t, newConnection(t), pastRuns()), keyRune('e'))

	opened := openHistory(t, editor)
	assert.Contains(t, opened.View(), historyTitle)

	assert.Equal(t, editor.View(), pressAll(t, opened, keyMsg(tea.KeyEscape)).View(),
		"esc returns to the layout as it was")
}

func TestCtrlOClosesTheHistoryItOpened(t *testing.T) {
	m := newHistoryModel(t, newConnection(t), pastRuns())

	assert.Contains(t, openHistory(t, openHistory(t, m)).View(), catalogTitle)
}

func TestHistoryListsWhatTheStoreServes(t *testing.T) {
	view := plain(openHistory(t, newHistoryModel(t, newConnection(t), pastRuns())).View())

	assert.Contains(t, view, "SELECT TOP 5 * FRM c")
	assert.Contains(t, view, "telemetry.events")
	assert.Contains(t, view, "SELECT * FROM c WHERE c.amount > 100")
	assert.Contains(t, view, "sales.orders")
}

func TestSlashFiltersTheHistory(t *testing.T) {
	m := openHistory(t, newHistoryModel(t, newConnection(t), pastRuns()))

	view := plain(pressAll(t, m, keyRune('/'), keyText("orders")).View())

	assert.Contains(t, view, "/ orders")
	assert.Contains(t, view, "c.amount")
	assert.NotContains(t, view, "TOP 5")
}

func TestEscapeClearsTheFilterBeforeItClosesTheOverlay(t *testing.T) {
	m := pressAll(t, openHistory(t, newHistoryModel(t, newConnection(t), pastRuns())), keyRune('/'), keyText("orders"))

	cleared := pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, cleared.View(), historyTitle, "the first esc only clears the filter")
	assert.Contains(t, plain(cleared.View()), "TOP 5")

	assert.Contains(t, pressAll(t, cleared, keyMsg(tea.KeyEscape)).View(), catalogTitle)
}

func TestQIsTextWhileFilteringAndQuitsOtherwise(t *testing.T) {
	m := openHistory(t, newHistoryModel(t, newConnection(t), pastRuns()))

	_, cmd := m.Update(keyRune('q'))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd(), "q quits from the overlay like any other")

	filtering := pressAll(t, m, keyRune('/'))
	typed, cmd := filtering.Update(keyRune('q'))
	assert.Nil(t, cmd)
	assert.Contains(t, plain(typed.View()), "/ q")
}

func TestEnterRecallsTheSelectedQueryAndItsScope(t *testing.T) {
	m := openHistory(t, newHistoryModel(t, newConnection(t), pastRuns()))

	recalled := pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))

	view := plain(recalled.View())
	assert.Contains(t, view, catalogTitle, "the overlay closes")
	assert.Contains(t, view, "SELECT * FROM c WHERE c.amount > 100", "the editor holds the query")
	assert.Contains(t, view, "sales.orders", "and the status bar the scope it ran in")

	assert.Contains(t, plain(pressAll(t, recalled, keyText(" AND 1 = 1")).View()), "> 100 AND 1 = 1",
		"the focus is on the editor, ready to change the query")
}

func TestEnterRecallsTheFirstMatchOfAFilter(t *testing.T) {
	m := openHistory(t, newHistoryModel(t, newConnection(t), pastRuns()))

	recalled := pressAll(t, m, keyRune('/'), keyText("amount"), keyMsg(tea.KeyEnter))

	assert.Contains(t, plain(recalled.View()), "SELECT * FROM c WHERE c.amount > 100")
	assert.Contains(t, recalled.View(), catalogTitle)
}

func TestCtrlRRecallsAndRunsTheSelectedQuery(t *testing.T) {
	conn := newConnection(t)
	m := openHistory(t, newHistoryModel(t, conn, pastRuns()))

	rerun := pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyCtrlR))

	require.Len(t, conn.queries, 1)
	assert.Equal(t, "SELECT * FROM c WHERE c.amount > 100", conn.queries[0].Text)
	assert.Equal(t, []string{"sales", "orders"}, conn.queries[0].Scope, "the recalled scope is the one it runs in")
	assert.Contains(t, rerun.View(), "item-1-0")
}

func TestRecallWithNothingToRecallStaysPut(t *testing.T) {
	conn := newConnection(t)
	m := openHistory(t, newHistoryModel(t, conn, &recordingStore{}))

	assert.Contains(t, pressAll(t, m, keyMsg(tea.KeyEnter)).View(), historyTitle)
	assert.Contains(t, pressAll(t, m, keyMsg(tea.KeyCtrlR)).View(), historyTitle)
	assert.Empty(t, conn.queries)
}

func TestAnEmptyHistorySaysSo(t *testing.T) {
	view := plain(openHistory(t, newHistoryModel(t, newConnection(t), &recordingStore{})).View())

	assert.Contains(t, view, "no queries recorded yet")
}

func TestAStoreThatCannotBeReadSaysSoInTheOverlay(t *testing.T) {
	store := &recordingStore{failRecent: errors.New("history.jsonl: permission denied")}

	view := plain(openHistory(t, newHistoryModel(t, newConnection(t), store)).View())

	assert.Contains(t, view, historyTitle)
	assert.Contains(t, view, "permission denied")
}
