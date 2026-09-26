package query_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const crossQuery = "SELECT d.name AS department, p.name AS product FROM hr.departments d CROSS JOIN sales.products p"

func crossContainers(t *testing.T, departments, products int) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"hr.departments": {page(t, 1, items("d", departments)...)},
		"sales.products": {page(t, 2, items("p", products)...)},
	})
}

func TestACrossJoinIsTheProductFirstInputRowMajor(t *testing.T) {
	pages := drain(t, execute(t, crossContainers(t, 2, 2), crossQuery))

	assert.Equal(t, []string{"department", "product"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"d 0", "p 0"}, {"d 0", "p 1"}, {"d 1", "p 0"}, {"d 1", "p 1"}}, rows(pages))
	assert.Equal(t, `{"d":{"department":"d 0"},"p":{"product":"p 0"}}`, string(pages[0].Raw[0]))
}

func TestACrossJoinIsServedInCappedPagesThatReadNothing(t *testing.T) {
	conn := crossContainers(t, 25, 100)
	cursor, err := query.Engine{Connection: conn, MaxJoinRows: 5000}.Execute(context.Background(), plan(t, crossQuery))
	require.NoError(t, err)

	pages := drain(t, cursor)

	var sizes []int
	for _, p := range pages {
		sizes = append(sizes, len(p.Rows))
	}
	assert.Equal(t, []int{1000, 1000, 500}, sizes)
	assert.InDelta(t, 3, pages[0].Stats.RequestCharge, 1e-9)
	assert.Zero(t, pages[1].Stats.RequestCharge)
	assert.Equal(t, 2, conn.opened)
}

func TestACrossJoinWithAnEmptySideIsEmpty(t *testing.T) {
	tests := []struct {
		name        string
		departments int
		products    int
		queried     []string
	}{
		{name: "empty first side", products: 3, queried: []string{"hr.departments"}},
		{name: "empty second side", departments: 3, queried: []string{"hr.departments", "sales.products"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := crossContainers(t, tt.departments, tt.products)
			cursor := execute(t, conn, crossQuery)

			pages := drain(t, cursor)

			assert.Empty(t, rows(pages))
			assert.False(t, cursor.HasMore())
			assert.Equal(t, tt.queried, queriedLabels(conn))
		})
	}
}

func TestACrossJoinPastTheCapFailsBeforeAnyPage(t *testing.T) {
	tests := []struct {
		name        string
		departments int
		products    int
		wantErr     string
	}{
		{name: "a side past the cap", departments: 3, products: 11, wantErr: "sales.products takes the rows held in memory past 10 (hr.departments 3)"},
		{name: "the product past the cap", departments: 3, products: 4, wantErr: "cross join of hr.departments (3 rows) and sales.products (4 rows) is 12 rows, past 10"},
		{name: "exactly at the cap", departments: 2, products: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := crossContainers(t, tt.departments, tt.products)
			cursor, err := query.Engine{Connection: conn, MaxJoinRows: 10}.Execute(context.Background(), plan(t, crossQuery))
			require.NoError(t, err)

			page, err := cursor.NextPage(context.Background())

			assert.Equal(t, conn.opened, conn.closed)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Len(t, page.Rows, 10)
				return
			}
			require.ErrorIs(t, err, query.ErrJoinTooLarge)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestACrossJoinFiltersEachSideByItsOwnConditions(t *testing.T) {
	conn := crossContainers(t, 1, 1)

	drain(t, execute(t, conn, crossQuery+` WHERE d.site = "Lima" AND p.price > 5`))

	assert.Equal(t, []adapter.Query{
		{Text: `SELECT * FROM d WHERE (d.site = "Lima")`, Scope: []string{"hr", "departments"}},
		{Text: "SELECT * FROM p WHERE (p.price > 5)", Scope: []string{"sales", "products"}},
	}, conn.queries)
}
