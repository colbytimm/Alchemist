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
)

// newWideModel is sized so the status bar has room for every field of a
// simulated run.
func newWideModel(t *testing.T, conn adapter.Connection, opts tui.Options) tea.Model {
	t.Helper()
	m, _ := newModelWith(t, conn, opts).Update(tea.WindowSizeMsg{Width: 3 * testWidth, Height: testHeight})
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
	assert.Equal(t, 2, conn.closed, "both leaf cursors are closed")
}

func TestASingleContainerRunClearsTheSimulatedBadge(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockJoin)

	m = runAnother(t, m, "SELECT * FROM sales.orders")

	assert.Contains(t, m.View(), "10 rows (+more)", "the second run did load")
	assert.NotContains(t, m.View(), simulatedBadge)
}
