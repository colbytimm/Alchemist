package query_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

// cteSales serves what the CTE bodies of these tests return: the service
// has already projected them.
func cteSales(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"sales.customers": {page(t, 3.1, `{"id":"c1","name":"Ada"}`, `{"id":"c2","name":"Grace"}`)},
		"sales.orders":    {page(t, 6.2, `{"customerId":"c1","id":"o1","total":120}`, `{"customerId":"c9","id":"o2","total":300}`)},
	})
}

func TestOpaqueCTEsAreInputsOfEveryJoinKind(t *testing.T) {
	const cte = `WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu WHERE cu.region = "west") ` +
		"SELECT o.id, west.name FROM sales.orders o "
	tests := []struct {
		name string
		join string
		want [][]string
	}{
		{name: "streamed, inner", join: "JOIN west ON o.customerId = west.id", want: [][]string{{"o1", "Ada"}}},
		{name: "held, left", join: "LEFT JOIN west ON o.customerId = west.id", want: [][]string{{"o1", "Ada"}, {"o2", ""}}},
		{name: "held, right", join: "RIGHT JOIN west ON o.customerId = west.id", want: [][]string{{"o1", "Ada"}, {"", "Grace"}}},
		{name: "held, full", join: "FULL JOIN west ON o.customerId = west.id", want: [][]string{{"o1", "Ada"}, {"o2", ""}, {"", "Grace"}}},
		{name: "streamed, left", join: `LEFT JOIN west ON o.customerId = west.id WHERE o.id != "x"`, want: [][]string{{"o1", "Ada"}, {"o2", ""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := drain(t, execute(t, cteSales(t), cte+tt.join))

			assert.ElementsMatch(t, tt.want, rows(pages))
		})
	}
}

func TestChargesOfCTEsAreFiledUnderTheirNames(t *testing.T) {
	total, leaves := charges(drain(t, execute(t, cteSales(t), westAndBig)))

	assert.InDelta(t, 9.3, total, 1e-9)
	assert.Equal(t, map[string]float64{"west (sales.customers)": 3.1, "big (sales.orders)": 6.2}, leaves)
}

func TestEmptyCTEsStillHaveTheColumnsTheirSelectListsName(t *testing.T) {
	conn := cteSales(t)
	conn.pages["sales.customers"] = []adapter.Page{page(t, 1)}

	pages := drain(t, execute(t, conn, "WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu) "+
		"SELECT * FROM sales.orders o LEFT JOIN west ON o.customerId = west.id"))

	assert.Equal(t, []string{"o.customerId", "o.id", "o.total", "west.id", "west.name"}, pages[0].Columns)
}

func hrContainers(t *testing.T) *containers {
	t.Helper()
	return newContainers(map[string][]adapter.Page{
		"hr.employees": {
			page(t, 2, `{"id":"e1","managerId":"e2","name":"Kim"}`, `{"id":"e2","name":"Lee"}`),
			page(t, 1, `{"id":"e3","managerId":"e2","name":"Ray"}`),
		},
	})
}

func TestOneCTEReadTwiceIsQueriedAndChargedOnce(t *testing.T) {
	conn := hrContainers(t)

	pages := drain(t, execute(t, conn, staff))

	total, leaves := charges(pages)
	assert.Len(t, conn.queries, 1)
	assert.InDelta(t, 3, total, 1e-9)
	assert.Equal(t, map[string]float64{"staff (hr.employees)": 3}, leaves)
	assert.ElementsMatch(t, [][]string{{"Kim", "Lee"}, {"Lee", ""}, {"Ray", "Lee"}}, rows(pages),
		"both readers see every row")
	assert.Equal(t, conn.opened, conn.closed)
}

func TestMaterializedCTEsCountOnceAgainstTheCap(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		wantErr bool
	}{
		{name: "its rows once fit", max: 3},
		{name: "its rows past the cap", max: 2, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := hrContainers(t)
			cursor, err := query.Engine{Connection: conn, MaxJoinRows: tt.max}.Execute(context.Background(), plan(t, staff))
			require.NoError(t, err)

			_, err = cursor.NextPage(context.Background())

			assert.Equal(t, conn.opened, conn.closed)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, query.ErrJoinTooLarge)
			assert.ErrorContains(t, err, "staff takes the rows held in memory past 2")
		})
	}
}

func TestComposedCTEsAreReadAsFlatItemsUnderTheirNames(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.archive":   {page(t, 1, `{"customerId":"c2","id":"a1","sku":"s9"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`, `{"id":"c2","name":"Grace"}`)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`)},
	})

	pages := drain(t, execute(t, conn, composed))

	assert.Equal(t, []string{"named.orderId", "named.customer", "product"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"o1", "Ada", "Lamp"}, {"a1", "Grace", ""}}, rows(pages))
	assert.Equal(t, `{"named":{"orderId":"o1","customer":"Ada"},"p":{"product":"Lamp"}}`, string(pages[0].Raw[0]))
	_, leaves := charges(pages)
	assert.Equal(t, map[string]float64{
		"everything (sales.orders)": 1, "everything (sales.archive)": 1,
		"named (sales.customers)": 1, "sales.products": 1,
	}, leaves)
}

func TestUnionCTEsCarryTheContainerColumn(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1"}`)},
		"sales.archive":   {page(t, 1, `{"customerId":"c1","id":"a1"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
	})

	pages := drain(t, execute(t, conn, "WITH every AS (SELECT * FROM sales.orders, sales.archive) "+
		"SELECT every._container, every.id, cu.name FROM every JOIN sales.customers cu ON every.customerId = cu.id"))

	assert.Equal(t, [][]string{{"sales.orders", "o1", "Ada"}, {"sales.archive", "a1", "Ada"}}, rows(pages))
}

func TestReadingComposedCTEsWholeServesTheirFlatItems(t *testing.T) {
	conn := cteSales(t)

	pages := drain(t, execute(t, conn, "WITH j AS (SELECT o.id AS orderId, cu.name FROM sales.orders o "+
		"LEFT JOIN sales.customers cu ON o.customerId = cu.id) SELECT * FROM j"))

	assert.Equal(t, []string{"orderId", "name"}, pages[0].Columns)
	assert.Equal(t, [][]string{{"o1", "Ada"}, {"o2", ""}}, rows(pages))
	assert.Equal(t, `{"orderId":"o2"}`, string(pages[0].Raw[1]))
}

func TestCancellingAMaterializationClosesItsLeaf(t *testing.T) {
	conn := hrContainers(t)
	ctx, cancel := context.WithCancel(context.Background())
	conn.onQuery = func(string) { cancel() }
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, staff))
	require.NoError(t, err)

	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, conn.opened)
	assert.Equal(t, 1, conn.closed)
}

func TestCancellingTheBuildOfNestedCTEsClosesEveryLeafCursor(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`)},
		"sales.archive":   {page(t, 1, `{"customerId":"c2","id":"a1","sku":"s9"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`)},
	})
	ctx, cancel := context.WithCancel(context.Background())
	conn.onQuery = func(label string) {
		if label == "sales.customers" {
			cancel()
		}
	}
	cursor, err := query.Engine{Connection: conn}.Execute(ctx, plan(t, composed))
	require.NoError(t, err)

	_, err = cursor.NextPage(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, conn.opened, conn.closed)
	assert.Equal(t, 1, conn.mostOpen)
}

func TestAtMostOneLeafIsOpenInsideNestedCTEs(t *testing.T) {
	conn := newContainers(map[string][]adapter.Page{
		"sales.orders":    {page(t, 1, `{"customerId":"c1","id":"o1","sku":"s1"}`), page(t, 1, `{"customerId":"c1","id":"o2","sku":"s1"}`)},
		"sales.archive":   {page(t, 1, `{"customerId":"c2","id":"a1","sku":"s9"}`)},
		"sales.customers": {page(t, 1, `{"id":"c1","name":"Ada"}`)},
		"sales.products":  {page(t, 1, `{"id":"s1","name":"Lamp"}`)},
	})
	cursor := execute(t, conn, composed)

	for first := true; first || cursor.HasMore(); first = false {
		_, err := cursor.NextPage(context.Background())
		require.NoError(t, err)
		assert.LessOrEqual(t, conn.opened-conn.closed, 1)
	}

	assert.Equal(t, 1, conn.mostOpen)
	assert.Equal(t, conn.opened, conn.closed)
}

func TestAMalformedTreeIsRefusedNotRun(t *testing.T) {
	join := joinOf(t, plan(t, leftJoin))
	leaves := plan(t, leftJoin).Leaves
	tests := []struct {
		name string
		root query.Node
	}{
		{name: "no root"},
		{name: "a leaf out of range", root: &query.Scan{Leaf: 2}},
		{name: "a union of nothing", root: &query.Union{}},
		{name: "a flatten over SELECT *", root: &query.Flatten{Input: join}},
		{name: "a cross join of three inputs", root: &query.Join{
			Inputs: []query.Source{join.Inputs[0], join.Inputs[1], {Alias: "x", Rows: &query.Scan{}}},
			Steps:  []query.JoinStep{{Kind: query.CrossJoin}, {Kind: query.CrossJoin}},
		}},
		{name: "an unknown join kind", root: &query.Join{Inputs: join.Inputs, Steps: []query.JoinStep{{Kind: 9}}}},
		{name: "a key through an unbound alias", root: &query.Join{Inputs: join.Inputs, Steps: []query.JoinStep{{LeftKey: ref("z", "k"), RightKey: ref("cu", "k")}}}},
		{name: "an absent test of an unbound alias", root: &query.Join{Inputs: join.Inputs, Steps: join.Steps, Absent: []string{"z"}}},
		{name: "a join read as an input", root: &query.Join{Inputs: []query.Source{{Alias: "j", Rows: join}}, Columns: []query.JoinColumn{{Alias: "j", Field: "id"}}}},
		{name: "a lone input with nothing to do", root: &query.Join{Inputs: join.Inputs[:1]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.Engine{Connection: cteSales(t)}.Execute(context.Background(), query.Plan{Leaves: leaves, Root: tt.root})

			require.Error(t, err)
		})
	}
}
