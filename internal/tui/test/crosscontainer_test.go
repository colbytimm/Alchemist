package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

const (
	simulatedBadge = "simulated (client-side)"
	mockJoin       = "SELECT o.id, cu.note FROM sales.orders o JOIN sales.customers cu ON o.pk = cu.pk"
	mockThreeWay   = mockJoin + " JOIN sales.products p ON o.pk = p.pk"
	// threeWayCharge is what the first page of mockThreeWay costs: both held
	// sides whole and one page of orders.
	threeWayCharge = "17.50 RU (sales.customers 7.50 + sales.orders 2.50 + sales.products 7.50)"
)

// newWideModel is sized so the status bar has room for every field of a
// simulated run.
func newWideModel(t *testing.T, conn adapter.Connection, opts tui.Options) tea.Model {
	t.Helper()
	return newModelOfWidth(t, conn, opts, 3*testWidth)
}

func newModelOfWidth(t *testing.T, conn adapter.Connection, opts tui.Options, width int) tea.Model {
	t.Helper()
	m, _ := newModelWith(t, conn, opts).Update(tea.WindowSizeMsg{Width: width, Height: testHeight})
	model, _ := settle(m, m.Init())
	return model
}

func TestAContainerListRunsAgainstEachContainerAndTagsTheRows(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, newWideModel(t, conn, tui.Options{}), "SELECT * FROM sales.orders, sales.customers")

	view := m.View()
	assert.Contains(t, view, "_container")
	assert.Contains(t, view, "sales.orders")
	assert.Contains(t, view, simulatedBadge)
	require.Len(t, conn.queries, 1, "the second container waits for the first to run out")
	assert.Equal(t, "SELECT * FROM c", conn.queries[0].Text)
}

func TestFetchingMoreOfAUnionCrossesIntoTheNextContainer(t *testing.T) {
	conn := newConnection(t)
	m := runQuery(t, newWideModel(t, conn, tui.Options{}), "SELECT * FROM sales.orders, sales.customers")

	m = pressAll(t, focusResults(t, m), keyRune('m'), keyRune('m'), keyRune('m'))

	view := m.View()
	assert.Contains(t, view, "40 rows (+more)")
	assert.Contains(t, view, "10.00 RU (sales.customers 2.50 + sales.orders 7.50)")
	assert.Equal(t, []string{"sales", "customers"}, conn.queries[1].Scope)
}

func TestAJoinReportsTheSummedChargeOfBothContainers(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockJoin)

	view := m.View()
	assert.Contains(t, view, "o.id")
	assert.Contains(t, view, "cu.note")
	assert.Contains(t, view, "102 rows (+more)", "each of ten orders matches the customers sharing its pk")
	assert.Contains(t, view, "10.00 RU (sales.customers 7.50 + sales.orders 2.50)")
	assert.Contains(t, view, simulatedBadge)
}

func TestAJoinOverTheRowCapFailsWithTheFixAndRendersNothing(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, newWideModel(t, conn, tui.Options{Accounts: []tui.Account{{Name: mock.Name, MaxJoinRows: 5}}}), mockJoin)

	view := plain(m.View())
	assert.Contains(t, view, "WHERE filter")
	assert.NotContains(t, view, "item-1-0")
	require.Len(t, conn.queries, 1, "the streamed side is never opened")
	assert.Equal(t, 1, conn.closed, "the held side's cursor is closed")
}

func TestASingleContainerRunClearsTheSimulatedBadge(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockJoin)

	m = runAnother(t, m, "SELECT * FROM sales.orders")

	assert.Contains(t, m.View(), "10 rows (+more)", "the second run did load")
	assert.NotContains(t, m.View(), simulatedBadge)
}

func TestAThreeContainerJoinTotalsEveryLeafCharge(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockThreeWay)

	view := m.View()
	assert.Contains(t, view, "o.id")
	assert.Contains(t, view, "cu.note")
	assert.Contains(t, view, simulatedBadge)
	assert.Contains(t, view, threeWayCharge)
}

func TestANarrowStatusBarFoldsTheBreakdownAndKeepsTheElapsedTime(t *testing.T) {
	m := runQuery(t, newModelOfWidth(t, newConnection(t), tui.Options{}, 120), mockThreeWay)

	view := plain(m.View())
	assert.Contains(t, view, "1,000 rows (+more)")
	assert.Regexp(t, `17\.50 RU \(3 containers\) \S+ [0-9.]+(ns|µs|ms|s) `, view,
		"the folded charge is followed by the elapsed time")
}

func TestATotalOverTheRowCapFailsNamingTheSideThatCrossedIt(t *testing.T) {
	conn := newConnection(t)
	opts := tui.Options{Accounts: []tui.Account{{Name: mock.Name, MaxJoinRows: 50}}}

	m := runQuery(t, newWideModel(t, conn, opts), mockThreeWay)

	view := plain(m.View())
	assert.Contains(t, view, "sales.products takes the rows held in memory past 50 (sales.customers 30)")
	assert.NotContains(t, view, "item-1-0")
	assert.Equal(t, len(conn.queries), conn.closed, "every leaf cursor opened is closed")
}

func TestFetchingMoreOfAFannedOutJoinServesRowsAlreadyRead(t *testing.T) {
	conn := newConnection(t)
	m := runQuery(t, newWideModel(t, conn, tui.Options{}), mockThreeWay)
	require.Contains(t, m.View(), "1,000 rows (+more)")

	m = pressAll(t, focusResults(t, m), keyRune('m'))

	assert.Contains(t, m.View(), "1,062 rows (+more)")
	assert.Contains(t, m.View(), threeWayCharge, "the appended rows cost nothing more")
	assert.Len(t, conn.queries, 3, "no leaf is queried again")
}

func TestARefusedMultiWayShapeIsShownAndRecorded(t *testing.T) {
	store := &recordingStore{}
	refused := mockJoin + " CROSS JOIN sales.products p"

	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{History: store}), refused)

	assert.Contains(t, plain(m.View()), "CROSS JOIN in a chain")
	require.Len(t, store.entries, 1)
	assert.False(t, store.entries[0].OK)
	assert.Contains(t, store.entries[0].Error, "CROSS JOIN in a chain")
}
