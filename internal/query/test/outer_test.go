package query_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	leftList = "SELECT o.id, cu.name FROM sales.orders o LEFT JOIN sales.customers cu ON o.customerId = cu.id"
	// holdLeft filters the orders, so the customers stream and the orders,
	// the side LEFT preserves, are the ones held.
	holdLeft = ` WHERE o.id != "none"`
)

func unmatchedSales(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"sales.orders": {page(t, 2,
			`{"customerId":"c1","id":"o1"}`,
			`{"customerId":"c9","id":"o2"}`,
			`{"id":"o3"}`,
			`{"customerId":"c1","id":"o4"}`,
			`{"customerId":null,"id":"o5"}`)},
		"sales.customers": {page(t, 1,
			`{"id":"c1","name":"Ada"}`,
			`{"id":"c2","name":"Grace"}`)},
	})
}

func TestALeftJoinPadsEveryUnmatchedLeftRow(t *testing.T) {
	pages := drain(t, execute(t, unmatchedSales(t), leftList))

	assert.Equal(t, []string{"o.id", "cu.name"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"o1", "Ada"}, {"o2", ""}, {"o3", ""}, {"o4", "Ada"}, {"o5", ""}}, rows(pages),
		"a customer that does not exist, a missing key and a null key match nothing, and are padded")
}

func TestAPaddedRowOmitsTheAbsentSideFromItsRawItem(t *testing.T) {
	pages := drain(t, execute(t, unmatchedSales(t), leftList))

	assert.Equal(t, `{"o":{"id":"o1"},"cu":{"name":"Ada"}}`, string(pages[0].Raw[0]))
	assert.Equal(t, `{"o":{"id":"o2"}}`, string(pages[0].Raw[1]))
}

func TestALeftJoinYieldsTheSameRowsWhicheverSideIsHeld(t *testing.T) {
	streamedLeft := drain(t, execute(t, unmatchedSales(t), leftList))
	conn := unmatchedSales(t)
	heldLeft := drain(t, execute(t, conn, leftList+holdLeft))

	require.Equal(t, []string{"sales.orders", "sales.customers"}, queriedLabels(conn), "the orders are held")
	assert.ElementsMatch(t, rows(streamedLeft), rows(heldLeft))
	assert.Equal(t, [][]string{{"o1", "Ada"}, {"o4", "Ada"}, {"o2", ""}, {"o3", ""}, {"o5", ""}}, rows(heldLeft),
		"the held side's padded rows come last, in the order they were read")
}

func TestARightJoinPadsEveryUnmatchedRightRow(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"telemetry.alerts":  {page(t, 1, `{"deviceId":"d1","message":"hot"}`, `{"deviceId":"d1","message":"cold"}`, `{"deviceId":"d7","message":"lost"}`)},
		"telemetry.devices": {page(t, 1, `{"id":"d1","site":"north"}`, `{"id":"d2","site":"south"}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT a.message, d.site FROM telemetry.alerts a RIGHT JOIN telemetry.devices d ON a.deviceId = d.id"))

	assert.Equal(t, [][]string{{"hot", "north"}, {"cold", "north"}, {"", "south"}}, rows(pages))
	assert.Equal(t, `{"d":{"site":"south"}}`, string(pages[len(pages)-1].Raw[len(pages[len(pages)-1].Raw)-1]))
}

func TestAnOuterJoinWithAnEmptySide(t *testing.T) {
	tests := []struct {
		name      string
		orders    []string
		customers []string
		text      string
		want      [][]string
	}{
		{name: "left join, empty right", orders: []string{`{"customerId":"c1","id":"o1"}`}, text: leftList, want: [][]string{{"o1", ""}}},
		{name: "left join, empty left", customers: []string{`{"id":"c1","name":"Ada"}`}, text: leftList},
		{name: "left join, both empty", text: leftList},
		{name: "left join held, empty right", orders: []string{`{"customerId":"c1","id":"o1"}`}, text: leftList + holdLeft, want: [][]string{{"o1", ""}}},
		{name: "left join held, empty left", customers: []string{`{"id":"c1","name":"Ada"}`}, text: leftList + holdLeft},
		{name: "full join, empty left", customers: []string{`{"id":"c1","name":"Ada"}`}, text: fullList, want: [][]string{{"", "Ada"}}},
		{name: "full join, empty right", orders: []string{`{"customerId":"c1","id":"o1"}`}, text: fullList, want: [][]string{{"o1", ""}}},
		{name: "full join, both empty", text: fullList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newContainers(map[string][]adapter.Page{
				"sales.orders":    {page(t, 1, tt.orders...)},
				"sales.customers": {page(t, 1, tt.customers...)},
			})
			cursor := execute(t, conn, tt.text)

			pages := drain(t, cursor)

			assert.Equal(t, tt.want, rows(pages))
			assert.False(t, cursor.HasMore())
			assert.Equal(t, conn.opened, conn.closed)
		})
	}
}

func TestSelectStarKeepsAnUnmatchedHeldSidesColumns(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c9","id":"o1"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn, leftJoin))

	assert.Equal(t, []string{"o.customerId", "o.id", "cu.id", "cu.name"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"c9", "o1", "", ""}}, rows(pages))
}

func TestDuplicateKeysOnEitherSideOfALeftJoin(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1"}`, `{"customerId":"c1","id":"o2"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`, `{"id":"c1","name":"Ada L."}`)},
	})

	pages := drain(t, execute(t, conn, leftList))

	assert.Equal(t, [][]string{{"o1", "Ada"}, {"o1", "Ada L."}, {"o2", "Ada"}, {"o2", "Ada L."}}, rows(pages))
}

const fullList = "SELECT o.id, cu.name FROM sales.orders o FULL JOIN sales.customers cu ON o.customerId = cu.id"

func TestAFullJoinServesEveryUnmatchedRowOfBothSidesOnce(t *testing.T) {
	pages := drain(t, execute(t, unmatchedSales(t), fullList))

	assert.Equal(t, [][]string{
		{"o1", "Ada"}, {"o2", ""}, {"o3", ""}, {"o4", "Ada"}, {"o5", ""},
		{"", "Grace"},
	}, rows(pages))
}

func TestTheFlushOfAFullJoinIsAPageOfItsOwn(t *testing.T) {
	conn := unmatchedSales(t)
	cursor := execute(t, conn, fullList)

	first, err := cursor.NextPage(context.Background())
	require.NoError(t, err)
	require.True(t, cursor.HasMore(), "the stream has ended, and a customer is still to flush")
	flush, err := cursor.NextPage(context.Background())
	require.NoError(t, err)

	assert.Len(t, first.Rows, 5)
	assert.Equal(t, [][]string{{"", "Grace"}}, flush.Rows)
	assert.Zero(t, flush.Stats.RequestCharge)
	assert.Empty(t, flush.Stats.LeafCharges)
	assert.False(t, cursor.HasMore())
	assert.Equal(t, 2, conn.opened, "the flush reads nothing")
}

func TestAFullJoinWithNothingToFlushEndsWithTheStream(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
	})
	cursor := execute(t, conn, fullList)

	pages := drain(t, cursor)

	assert.Len(t, pages, 1)
	assert.Equal(t, [][]string{{"o1", "Ada"}}, rows(pages))
}

func TestALargeFlushSpansCappedPages(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"none","id":"o1"}`)},
		"sales.customers": {page(t, 1, items("c", 2500)...)},
	})

	pages := drain(t, execute(t, conn, fullList))

	var sizes []int
	for _, p := range pages {
		sizes = append(sizes, len(p.Rows))
	}
	assert.Equal(t, []int{1, 1000, 1000, 500}, sizes)
	assert.Len(t, uniqueRows(rows(pages)), 2501, "every row is served exactly once")
}

func uniqueRows(all [][]string) map[string]bool {
	seen := map[string]bool{}
	for _, row := range all {
		seen[strings.Join(row, "|")] = true
	}
	return seen
}

func TestAnOuterJoinChargesTheSumOfItsLeaves(t *testing.T) {
	total, leaves := charges(drain(t, execute(t, unmatchedSales(t), fullList)))

	assert.InDelta(t, 3, total, 1e-9)
	assert.Equal(t, map[string]float64{"sales.orders": 2, "sales.customers": 1}, leaves)
}

func TestAnOuterChainDropsWhatALaterInnerStepDrops(t *testing.T) {
	conn := unmatchedSales(t)
	conn.pages["sales.regions"] = []adapter.Page{page(t, 1, `{"id":"west","zone":"W"}`)}
	conn.pages["sales.customers"] = []adapter.Page{page(t, 1,
		`{"id":"c1","name":"Ada","region":"west"}`,
		`{"id":"c2","name":"Grace","region":"east"}`)}

	pages := drain(t, execute(t, conn, "SELECT o.id, cu.name, r.zone FROM sales.orders o "+
		"LEFT JOIN sales.customers cu ON o.customerId = cu.id JOIN sales.regions r ON cu.region = r.id"))

	assert.Equal(t, [][]string{{"o1", "Ada", "W"}, {"o4", "Ada", "W"}}, rows(pages),
		"an order with no customer has no region to join on")
}

func TestAnOuterChainStreamsItsFirstInputEvenWhenItIsFiltered(t *testing.T) {
	conn := unmatchedSales(t)
	conn.pages["sales.products"] = []adapter.Page{page(t, 1, `{"id":"s1"}`)}

	drain(t, execute(t, conn, leftJoin+` JOIN sales.products p ON o.sku = p.id WHERE o.id = "o1"`))

	assert.Equal(t, []string{"sales.customers", "sales.products", "sales.orders"}, queriedLabels(conn))
}

func TestAnEmptyInnerStepBeforeARightStepStillReturnsTheRightRows(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.customers": {page(t, 1)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`, `{"id":"s2","name":"Desk"}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT o.id, p.name FROM sales.orders o "+
		"JOIN sales.customers cu ON o.customerId = cu.id RIGHT JOIN sales.products p ON o.sku = p.id"))

	assert.Equal(t, [][]string{{"", "Lamp"}, {"", "Desk"}}, rows(pages))
	assert.Equal(t, []string{"sales.customers", "sales.products"}, queriedLabels(conn),
		"every order would be dropped at the empty step, so none is read")
}

func TestAnEmptyInnerStepBeforeALeftStepEndsTheRunEarly(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.customers": {page(t, 1)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`)},
	})
	cursor := execute(t, conn, "SELECT o.id, p.name FROM sales.orders o "+
		"JOIN sales.customers cu ON o.customerId = cu.id LEFT JOIN sales.products p ON o.sku = p.id")

	pages := drain(t, cursor)

	assert.Empty(t, rows(pages))
	assert.False(t, cursor.HasMore())
	assert.Equal(t, []string{"sales.customers"}, queriedLabels(conn))
}

func TestFlushedRowsOfOneStepMatchTheRowsOfTheNext(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"x.a": {page(t, 1, `{"id":"a1","k":1}`)},
		"x.b": {page(t, 1, `{"id":"b1","j":"x","k":1}`, `{"id":"b2","j":"y","k":2}`)},
		"x.c": {page(t, 1, `{"id":"c1","j":"x"}`, `{"id":"c2","j":"y"}`, `{"id":"c3","j":"z"}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT a.id, b.id, c.id FROM x.a a "+
		"RIGHT JOIN x.b b ON a.k = b.k RIGHT JOIN x.c c ON b.j = c.j"))

	assert.Equal(t, [][]string{{"a1", "b1", "c1"}, {"", "b2", "c2"}, {"", "", "c3"}}, rows(pages),
		"b2, flushed by the first step, matches c2 in the second, which is then not flushed")
}

func TestNoPageButTheLastIsEmpty(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"x.a": {page(t, 1, `{"id":"a1","k":1}`), page(t, 1, `{"id":"a2","k":9}`)},
		"x.b": {page(t, 1, `{"id":"b1","k":1}`)},
		"x.c": {page(t, 1, `{"id":"c1","k":5}`)},
	})

	pages := drain(t, execute(t, conn, "SELECT a.id, b.id, c.id FROM x.a a "+
		"JOIN x.b b ON a.k = b.k FULL JOIN x.c c ON b.k = c.k"))

	for _, p := range pages[:len(pages)-1] {
		assert.NotEmpty(t, p.Rows)
	}
	assert.Equal(t, [][]string{{"a1", "b1", ""}, {"", "", "c1"}}, rows(pages))
}

func TestTheAbsentSideTestKeepsOnlyThePaddedRows(t *testing.T) {
	pages := drain(t, execute(t, unmatchedSales(t), leftList+" WHERE NOT IS_DEFINED(cu)"))

	assert.Equal(t, [][]string{{"o2", ""}, {"o3", ""}, {"o5", ""}}, rows(pages))
}

func TestTheAbsentSideTestLeavesAPreservedSideFilterToItsLeaf(t *testing.T) {
	conn := unmatchedSales(t)

	drain(t, execute(t, conn, leftList+` WHERE NOT IS_DEFINED(cu) AND o.id != "o3"`))

	assert.Equal(t, `SELECT * FROM o WHERE (o.id != "o3")`, conn.queries[0].Text)
}

func TestTheAbsentSideTestOfAnApplyFiltersACrossJoin(t *testing.T) {
	const crossed = "SELECT o.id, cu.id AS c FROM sales.orders o OUTER APPLY l IN o.lines " +
		"CROSS JOIN sales.customers cu WHERE NOT IS_DEFINED(l)"
	tests := []struct {
		name  string
		input string
	}{
		{name: "in the main query", input: crossed},
		{name: "in a CTE read whole", input: "WITH j AS (" + crossed + ") SELECT * FROM j"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newContainers(map[string][]adapter.Page{
				"sales.orders":    {page(t, 1, `{"id":"o1","lines":[{"sku":"s1"}]}`, `{"id":"o2"}`)},
				"sales.customers": {page(t, 1, `{"id":"c1"}`, `{"id":"c2"}`)},
			})

			pages := drain(t, execute(t, conn, tt.input))

			assert.Equal(t, [][]string{{"o2", "c1"}, {"o2", "c2"}}, rows(pages), "o1 has a line")
		})
	}
}

func TestTheAbsentSideTestOnAFullJoinKeepsTheUnmatchedCustomers(t *testing.T) {
	pages := drain(t, execute(t, unmatchedSales(t), fullList+" WHERE NOT IS_DEFINED(o)"))

	assert.Equal(t, [][]string{{"", "Grace"}}, rows(pages))
}

func TestCancellingDuringTheFlushClosesEveryLeafCursor(t *testing.T) {
	conn := unmatchedSales(t)
	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, fullList))
	require.NoError(t, err)
	_, err = cursor.NextPage(ctx)
	require.NoError(t, err)

	cancel()
	_, err = cursor.NextPage(ctx)

	require.NoError(t, err, "the flush reads nothing, so it has nothing to cancel")
	assert.Equal(t, conn.opened, conn.closed)
	require.NoError(t, cursor.Close())
	assert.Equal(t, 2, conn.closed, "closing again closes nothing twice")
}

func TestTheOuterJoinRowCapCountsTheHeldSide(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c0","id":"o1"}`)},
		"sales.customers": {page(t, 1, items("c", 11)...)},
	})
	cursor, err := query.Engine{Connection: conn, MaxJoinRows: 10}.Execute(context.Background(), plan(t, fullList))
	require.NoError(t, err)

	_, err = cursor.NextPage(context.Background())

	require.ErrorIs(t, err, query.ErrJoinTooLarge)
	assert.ErrorContains(t, err, fmt.Sprintf("sales.customers takes the rows held in memory past %d", 10))
	assert.Equal(t, conn.opened, conn.closed)
}
