package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

// No order's id is a customer's pk, so every order of these joins is padded.
const (
	mockLeftJoin = "SELECT o.id, cu.note FROM sales.orders o LEFT JOIN sales.customers cu ON o.id = cu.pk"
	mockFullJoin = "SELECT o.id, cu.id AS customer FROM sales.orders o FULL JOIN sales.customers cu ON o.id = cu.pk"
)

func TestALeftJoinRendersItsPaddedRowsUnderTheBadge(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockLeftJoin)

	view := plain(m.View())
	assert.Contains(t, view, "o.id")
	assert.Contains(t, view, "cu.note")
	assert.Contains(t, view, "item-1-0")
	assert.NotContains(t, view, "row 0 of page 1", "no customer matched")
	assert.Contains(t, view, simulatedBadge)
	assert.Contains(t, view, "10.00 RU (sales.customers 7.50 + sales.orders 2.50)")
}

func TestFetchingMoreAfterAFullJoinsStreamServesTheFlushWithoutAQuery(t *testing.T) {
	conn := newConnection(t, mock.WithPages(1))
	m := runQuery(t, newWideModel(t, conn, tui.Options{}), mockFullJoin)
	require.Contains(t, m.View(), "10 rows (+more)", "the customers nothing matched are still to come")

	m = pressAll(t, focusResults(t, m), keyRune('m'))

	assert.Contains(t, m.View(), "20 rows")
	assert.NotContains(t, m.View(), "(+more)")
	assert.Len(t, conn.queries, 2, "the flush reads nothing")
}

func TestARefusedShapeShowsWhatToWriteInsteadAndIsRecorded(t *testing.T) {
	tests := []struct {
		name  string
		query string
		hint  string
	}{
		{name: "a condition on the optional side", query: mockLeftJoin + " WHERE cu.note = 'x'", hint: "write it in ON"},
		{name: "an expression in the SELECT list", query: "SELECT ROUND(o.amount) FROM sales.orders o JOIN sales.customers cu ON o.pk = cu.pk", hint: "put it in a CTE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingStore{}
			conn := newConnection(t)

			m := runQuery(t, newWideModel(t, conn, tui.Options{History: store}), tt.query)

			assert.Contains(t, strings.Join(strings.Fields(plain(m.View())), " "), tt.hint)
			assert.Empty(t, conn.queries)
			require.Len(t, store.entries, 1)
			assert.False(t, store.entries[0].OK)
		})
	}
}

func TestACrossJoinPastTheCapRendersNothingAndTheSessionGoesOn(t *testing.T) {
	conn := newConnection(t)
	opts := tui.Options{Accounts: []tui.Account{{Name: mock.Name, MaxJoinRows: 100}}}

	m := runQuery(t, newWideModel(t, conn, opts), "SELECT o.id, cu.id AS c FROM sales.orders o CROSS JOIN sales.customers cu")

	view := strings.Join(strings.Fields(plain(m.View())), " ")
	assert.Contains(t, view, "cross join of sales.orders (30 rows) and sales.customers (30 rows) is 900 rows, past 100")
	assert.NotContains(t, view, "item-1-0")
	assert.Equal(t, len(conn.queries), conn.closed)

	m = runAnother(t, m, "SELECT * FROM sales.orders")
	assert.Contains(t, m.View(), "item-1-0")
}

func TestJoiningCTEsFilesTheirChargesUnderTheirNames(t *testing.T) {
	m := runQuery(t, newWideModel(t, newConnection(t), tui.Options{}),
		"WITH west AS (SELECT cu.id, cu.pk FROM sales.customers cu) "+
			"SELECT o.id, west.id AS customer FROM sales.orders o JOIN west ON o.pk = west.pk")

	view := plain(m.View())
	assert.Contains(t, view, "west (sales.customers) 7.50")
	assert.Contains(t, view, simulatedBadge)
}

func TestReadingWholeCTEsRunsTheirBodiesAsTheServiceWould(t *testing.T) {
	conn := newConnection(t)

	m := runQuery(t, newWideModel(t, conn, tui.Options{}), "WITH recent AS (SELECT * FROM sales.orders o) SELECT * FROM recent")

	view := plain(m.View())
	assert.Contains(t, view, "item-1-0")
	assert.NotContains(t, view, "recent.id", "the columns are the body's own")
	assert.NotContains(t, view, simulatedBadge)
	require.Len(t, conn.queries, 1)
	assert.Equal(t, "SELECT * FROM o", conn.queries[0].Text)
}

func TestExportingAPaddedRowLeavesTheAbsentSideOut(t *testing.T) {
	dir := t.TempDir()
	m := focusResults(t, runQuery(t, newWideModel(t, newConnection(t), tui.Options{}), mockLeftJoin))

	exportWriting(t, m, filepath.Join(dir, "padded.json"))
	exportWriting(t, m, filepath.Join(dir, "padded.csv"))

	documents := exportedDocuments(t, filepath.Join(dir, "padded.json"))
	require.NotEmpty(t, documents)
	assert.JSONEq(t, `{"o":{"id":"item-1-0"}}`, string(documents[0]))
	data, err := os.ReadFile(filepath.Join(dir, "padded.csv"))
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	assert.Equal(t, "o.id,cu.note", lines[0])
	assert.Equal(t, "item-1-0,", lines[1])
}

// exportWriting is exportTo, waiting on the write however long it takes.
func exportWriting(t *testing.T, m tea.Model, path string) {
	t.Helper()
	typed := pressAll(t, openExport(t, m), keyMsg(tea.KeyCtrlU), keyText(path))
	pressDisk(t, typed, keyMsg(tea.KeyEnter))
}
