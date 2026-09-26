package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

// The overlay title as it appears inside the top border.
const detailTitle = " Document "

// selectContainer walks the catalog to sales.orders and selects it, which is
// what publishes a default scope for queries that name none.
func selectContainer(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyEnter), keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))
}

// typeQuery fills the editor with text and leaves the focus there.
func typeQuery(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return pressAll(t, m, keyRune('e'), keyText(text))
}

func runQuery(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	return pressAll(t, typeQuery(t, m, text), keyMsg(tea.KeyCtrlR))
}

// focusResults moves from the editor, where running a query leaves the focus,
// to the pane that owns the rows.
func focusResults(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return pressAll(t, m, keyMsg(tea.KeyTab))
}

func TestRunningAQueryRendersItsFirstPage(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c")

	view := m.View()
	assert.Contains(t, view, "item-1-0", "the first row of the first page")
	assert.Contains(t, view, "10 rows (+more)")
	assert.Contains(t, view, "2.50 RU")
	assert.Contains(t, view, "5ms")
	require.Len(t, conn.queries, 1)
	assert.Equal(t, []string{"sales", "orders"}, conn.queries[0].Scope)
}

func TestAQueryInFlightSaysItIsRunning(t *testing.T) {
	m := typeQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c")

	running, _ := m.Update(keyMsg(tea.KeyCtrlR))

	view := running.View()
	assert.Contains(t, view, theme.Icons().SpinnerFrames[0], "the status bar spins while the query runs")
	assert.Contains(t, view, "— rows", "with no statistics until the first page lands")
}

func TestFetchingMoreAppendsPagesUntilTheCursorRunsOut(t *testing.T) {
	m := runQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c")

	// The mock serves three pages; the fourth press has nothing left to ask
	// for and must not try.
	m = pressAll(t, focusResults(t, m), keyRune('m'), keyRune('m'), keyRune('m'))

	view := m.View()
	assert.Contains(t, view, "30 rows")
	assert.NotContains(t, view, "(+more)", "the last page clears the indicator")
	assert.Contains(t, view, "7.50 RU", "the charge is the sum across the pages fetched")
	assert.Contains(t, view, "15ms")
}

func TestFetchingMoreIgnoresASecondRequestWhileOneIsInFlight(t *testing.T) {
	conn := newConnection(t)
	m := focusResults(t, runQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c"))

	fetching, fetchCmd := m.Update(keyRune('m'))
	ignored, cmd := fetching.Update(keyRune('m'))

	assert.Nil(t, cmd, "the cursor is with the fetch already running")
	settle(ignored, fetchCmd)
	assert.Equal(t, 2, conn.pageReads, "the opening page and one fetch-more, never a second in flight")
}

func TestAFailedFetchMoreKeepsTheRowsAlreadyOnScreen(t *testing.T) {
	conn := newConnection(t)
	m := focusResults(t, runQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c"))
	conn.failPage = 1

	m = pressAll(t, m, keyRune('m'))

	view := m.View()
	assert.Contains(t, view, "page unreachable", "the failure is reported")
	assert.Contains(t, view, "item-1-0", "the rows already fetched stay")
	assert.Contains(t, view, "10 rows", "and so do their statistics")
	assert.NotContains(t, view, "(+more)", "but the cursor went with the failure")
}

func TestScrollingPastTheLastRowFetchesTheNextPage(t *testing.T) {
	m := focusResults(t, runQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c"))

	for range 10 {
		m = pressAll(t, m, keyMsg(tea.KeyDown))
	}

	assert.Contains(t, m.View(), "20 rows", "the press with nowhere left to go fetches")
}

func TestAFailedQueryShowsTheServiceErrorAndKeepsTheBuffer(t *testing.T) {
	conn := newConnection(t)
	conn.failQuery = 1

	m := runQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c")

	assert.Contains(t, m.View(), "unreachable", "the service message reaches the pane intact")
	assert.Contains(t, plain(m.View()), "SELECT * FROM c", "and the editor keeps the buffer")

	m = pressAll(t, m, keyMsg(tea.KeyCtrlR))

	assert.NotContains(t, m.View(), "unreachable", "a successful re-run clears the error")
	assert.Contains(t, m.View(), "item-1-0")
}

func TestAScopeWrittenInTheQueryBeatsTheCatalogSelection(t *testing.T) {
	conn := newConnection(t)

	runQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM telemetry.events AS e")

	require.Len(t, conn.queries, 1)
	assert.Equal(t, []string{"telemetry", "events"}, conn.queries[0].Scope)
	assert.Equal(t, "SELECT * FROM e", conn.queries[0].Text, "the source is rewritten to its alias")
}

func TestAQueryWithNoScopeAtAllIsNeverSent(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, newLoadedModel(t, conn), "SELECT * FROM c")

	assert.Contains(t, m.View(), "no container in scope")
	assert.Empty(t, conn.queries, "a query with nowhere to run must not reach the adapter")
}

func TestAShapeThatCannotBeSimulatedIsRefusedBeforeItReachesTheAdapter(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, selectContainer(t, newLoadedModel(t, conn)),
		"SELECT * FROM sales.orders o LEFT JOIN sales.customers cu ON o.pk = cu.pk")

	assert.Contains(t, plain(m.View()), "LEFT JOIN: not supported across containers")
	assert.Empty(t, conn.queries)
	assert.NotContains(t, m.View(), simulatedBadge, "nothing was simulated")
}

func TestRunningAnEmptyBufferSaysThereIsNothingToRun(t *testing.T) {
	conn := newConnection(t)

	m := pressAll(t, selectContainer(t, newLoadedModel(t, conn)), keyMsg(tea.KeyCtrlR))

	assert.Contains(t, m.View(), "nothing to run")
	assert.Empty(t, conn.queries)
}

func TestASecondRunDiscardsTheFirstAndClosesItsCursor(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c")

	first, firstCmd := m.Update(keyMsg(tea.KeyCtrlR))
	second, secondCmd := first.Update(keyMsg(tea.KeyCtrlR))

	superseded, _ := settle(second, firstCmd)
	assert.Contains(t, superseded.View(), "— rows", "the page of a replaced run must not render")
	assert.Equal(t, 1, conn.closed, "and its cursor is closed on arrival")

	current, _ := settle(superseded, secondCmd)
	assert.Contains(t, current.View(), "10 rows", "the run the user asked for lands")
	assert.Equal(t, 1, conn.closed)
}

func TestASecondRunCancelsTheContextOfTheFirst(t *testing.T) {
	conn := newConnection(t)
	m := typeQuery(t, selectContainer(t, newLoadedModel(t, conn)), "SELECT * FROM c")

	first, firstCmd := m.Update(keyMsg(tea.KeyCtrlR))
	second, _ := first.Update(keyMsg(tea.KeyCtrlR))
	settle(second, firstCmd)

	require.Len(t, conn.contexts, 1)
	assert.ErrorIs(t, conn.contexts[0].Err(), context.Canceled,
		"a replaced run is cancelled, so a slow adapter stops working on it")
}

func TestEnterOnARowOpensTheRawDocument(t *testing.T) {
	m := focusResults(t, runQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c"))

	opened := pressAll(t, m, keyMsg(tea.KeyEnter))

	view := opened.View()
	assert.Contains(t, view, detailTitle, "the overlay replaces the layout")
	assert.Contains(t, view, `"id": "item-1-0"`, "indented from the raw item")
	assert.NotContains(t, view, catalogTitle)

	assert.Contains(t, pressAll(t, opened, keyMsg(tea.KeyEscape)).View(), catalogTitle)
}

func TestTheDocumentOverlayFollowsTheRowCursor(t *testing.T) {
	m := focusResults(t, runQuery(t, selectContainer(t, newLoadedModel(t, newConnection(t))), "SELECT * FROM c"))

	opened := pressAll(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter))

	assert.Contains(t, opened.View(), `"id": "item-1-1"`)
}
