package query_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	joinQuery = `SELECT o.id, o.total, cu.name
FROM sales.orders AS o
JOIN sales.customers AS cu ON o.customerId = cu.id`
	joinEverything = "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"
)

func salesContainers(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"sales.orders": {
			page(t, 2, `{"customerId":"c1","id":"o1","total":10}`, `{"customerId":"c9","id":"o2","total":20}`),
			page(t, 3, `{"customerId":"c2","id":"o3","total":30}`),
		},
		"sales.customers": {
			page(t, 1.5, `{"id":"c1","name":"Ada"}`, `{"id":"c2","name":"Grace"}`),
		},
	})
}

func TestAJoinCombinesMatchingRowsUnderPrefixedColumns(t *testing.T) {
	pages := drain(t, execute(t, salesContainers(t), joinQuery))

	assert.Equal(t, []string{"o.id", "o.total", "cu.name"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"o1", "10", "Ada"}, {"o3", "30", "Grace"}}, rows(pages),
		"o2 has no customer and is dropped")
}

func TestAJoinNestsTheProjectedItemsUnderTheirAliases(t *testing.T) {
	pages := drain(t, execute(t, salesContainers(t), joinQuery))

	assert.JSONEq(t, `{"o":{"id":"o1","total":10},"cu":{"name":"Ada"}}`, string(pages[0].Raw[0]))
}

func TestARenamedJoinColumnTakesItsNewNameInTheHeaderAndTheItem(t *testing.T) {
	pages := drain(t, execute(t, salesContainers(t),
		"SELECT o.id, cu.name AS customer, cu.name FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"))

	assert.Equal(t, []string{"o.id", "customer", "cu.name"}, pages[0].Columns)
	assert.Equal(t, []string{"o1", "Ada", "Ada"}, pages[0].Rows[0])
	assert.JSONEq(t, `{"o":{"id":"o1"},"cu":{"customer":"Ada","name":"Ada"}}`, string(pages[0].Raw[0]))
}

func TestSelectStarJoinsEveryColumnOfBothSides(t *testing.T) {
	pages := drain(t, execute(t, salesContainers(t), joinEverything))

	assert.Equal(t, []string{"o.customerId", "o.id", "o.total", "cu.id", "cu.name"}, pages[0].Columns,
		"left side first, though the right one is read first")
	assert.Equal(t, []string{"c1", "o1", "10", "c1", "Ada"}, pages[0].Rows[0])
	assert.JSONEq(t,
		`{"o":{"customerId":"c1","id":"o1","total":10},"cu":{"id":"c1","name":"Ada"}}`,
		string(pages[0].Raw[0]))
}

func TestDuplicateKeysOnTheBuildSideYieldARowPerMatch(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","total":10}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`, `{"id":"c1","name":"Ada L."}`)},
	})

	pages := drain(t, execute(t, conn, joinQuery))

	assert.Equal(t, [][]string{{"o1", "10", "Ada"}, {"o1", "10", "Ada L."}}, rows(pages))
}

func TestAnEmptyBuildSideYieldsNoRowsAndNoError(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","total":10}`)},
		"sales.customers": {page(t, 1)},
	})
	cursor := execute(t, conn, joinQuery)

	pages := drain(t, cursor)

	assert.Empty(t, rows(pages))
	assert.False(t, cursor.HasMore())
	assert.Len(t, conn.queries, 1, "the streamed side is never queried")
	assert.Equal(t, conn.opened, conn.closed)
}

func TestProbePagesWithNoMatchAreSkippedRatherThanServedEmpty(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders": {
			page(t, 1, `{"customerId":"c9","id":"o1","total":10}`),
			page(t, 1, `{"customerId":"c1","id":"o2","total":20}`),
		},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn, joinQuery))

	require.Len(t, pages, 1)
	assert.Equal(t, [][]string{{"o2", "20", "Ada"}}, pages[0].Rows)
	assert.InDelta(t, 3, pages[0].Stats.RequestCharge, 1e-9, "the skipped page is still paid for")
}

func TestJoinKeysMatchByValueNotBySpelling(t *testing.T) {
	tests := []struct {
		name    string
		order   string
		matches bool
	}{
		{name: "integer and decimal", order: `{"customerId":1.0,"id":"o1","total":1}`, matches: true},
		{name: "exponent", order: `{"customerId":1e0,"id":"o1","total":1}`, matches: true},
		{name: "number and string", order: `{"customerId":"1","id":"o1","total":1}`, matches: false},
		{name: "missing key", order: `{"id":"o1","total":1}`, matches: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newContainers(map[string][]adapter.Page{
				"sales.orders":    {page(t, 1, tt.order)},
				"sales.customers": {page(t, 1, `{"id":1,"name":"Ada"}`)},
			})

			pages := drain(t, execute(t, conn, joinQuery))

			assert.Equal(t, tt.matches, len(rows(pages)) == 1)
		})
	}
}

func TestANegativeZeroJoinsZero(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":-0.0,"id":"o1","total":1}`)},
		"sales.customers": {page(t, 1, `{"id":0,"name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn, joinQuery))

	assert.Len(t, rows(pages), 1, "a join compares numbers as values, and -0 equals 0")
}

func TestIntegerKeysBeyondFloatPrecisionStayDistinct(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":9007199254740993,"id":"o1","total":1}`)},
		"sales.customers": {page(t, 1, `{"id":9007199254740992,"name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn, joinQuery))

	assert.Empty(t, rows(pages))
}

func TestAJoinKeyMayBeNested(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customer":{"id":"c1"},"id":"o1"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn,
		"SELECT o.id, cu.name FROM sales.orders o JOIN sales.customers cu ON o.customer.id = cu.id"))

	assert.Equal(t, [][]string{{"o1", "Ada"}}, rows(pages))
}

func TestAJoinChargesTheSumOfBothSides(t *testing.T) {
	var total float64
	leaves := map[string]float64{}
	for _, p := range drain(t, execute(t, salesContainers(t), joinQuery)) {
		total += p.Stats.RequestCharge
		for leaf, charge := range p.Stats.LeafCharges {
			leaves[leaf] += charge
		}
	}

	assert.InDelta(t, 6.5, total, 1e-9)
	assert.Equal(t, map[string]float64{"sales.orders": 5, "sales.customers": 1.5}, leaves)
}

func TestTheFilteredSideIsTheOneHeldInMemory(t *testing.T) {
	conn := salesContainers(t)
	engine := query.Engine{Connection: conn, MaxJoinRows: 1}

	cursor, err := engine.Execute(context.Background(), plan(t, joinQuery+` WHERE o.id = "o1"`))
	require.NoError(t, err)
	_, err = cursor.NextPage(context.Background())

	require.ErrorIs(t, err, query.ErrJoinTooLarge)
	assert.ErrorContains(t, err, "sales.orders", "orders carries the filter, so orders is built")
}

func TestABuildSideOverTheCapAbortsWithNothingPartial(t *testing.T) {
	conn := salesContainers(t)
	engine := query.Engine{Connection: conn, MaxJoinRows: 1}
	cursor, err := engine.Execute(context.Background(), plan(t, joinQuery))
	require.NoError(t, err)

	served, err := cursor.NextPage(context.Background())

	require.ErrorIs(t, err, query.ErrJoinTooLarge)
	assert.ErrorContains(t, err, "sales.customers")
	assert.Empty(t, served.Rows)
	assert.Len(t, conn.queries, 1, "the streamed side is never queried")
	assert.Equal(t, conn.opened, conn.closed)
}

func TestABuildSideExactlyAtTheCapIsJoined(t *testing.T) {
	engine := query.Engine{Connection: salesContainers(t), MaxJoinRows: 2}
	cursor, err := engine.Execute(context.Background(), plan(t, joinQuery))
	require.NoError(t, err)

	assert.Len(t, rows(drain(t, cursor)), 2)
}

func TestCancellingMidJoinClosesEveryLeafCursor(t *testing.T) {
	conn := salesContainers(t)
	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, joinQuery))
	require.NoError(t, err)
	_, err = cursor.NextPage(ctx)
	require.NoError(t, err)

	cancel()
	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 2, conn.opened)
	assert.Equal(t, 2, conn.closed)
}

func TestClosingAJoinTwiceClosesEachLeafOnce(t *testing.T) {
	conn := salesContainers(t)
	cursor := execute(t, conn, joinQuery)
	_, err := cursor.NextPage(context.Background())
	require.NoError(t, err)

	require.NoError(t, cursor.Close())
	require.NoError(t, cursor.Close())

	assert.Equal(t, 2, conn.closed)
}
