package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

const (
	westAndBig = `WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu WHERE cu.region = "west"),
     big  AS (SELECT o.id, o.customerId, ROUND(o.total) AS total
              FROM sales.orders o WHERE o.total > 100)
SELECT big.id, big.total, west.name
FROM big JOIN west ON big.customerId = west.id`
	staff = `WITH staff AS (SELECT e.id, e.name, e.managerId FROM hr.employees e)
SELECT w.name, m.name AS manager
FROM staff w LEFT JOIN staff m ON w.managerId = m.id`
	composed = `WITH everything AS (SELECT * FROM sales.orders, sales.archive AS c
                    WHERE c.status = "open"),
     named AS (SELECT o.id AS orderId, cu.name AS customer, o.sku
               FROM everything o JOIN sales.customers cu ON o.customerId = cu.id)
SELECT named.orderId, named.customer, p.name AS product
FROM named LEFT JOIN sales.products p ON named.sku = p.id`
)

func TestOpaqueCTEBodiesAreTheirLeafQueriesVerbatim(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "top and order by", body: "SELECT TOP 3 * FROM sales.orders o ORDER BY o.total DESC", want: "SELECT TOP 3 * FROM o ORDER BY o.total DESC"},
		{name: "group by", body: "SELECT o.customerId AS id, COUNT(1) AS n FROM sales.orders o GROUP BY o.customerId", want: "SELECT o.customerId AS id, COUNT(1) AS n FROM o GROUP BY o.customerId"},
		{name: "a function", body: "SELECT o.id, ROUND(o.total) AS total FROM sales.orders o", want: "SELECT o.id, ROUND(o.total) AS total FROM o"},
		{name: "a property join", body: "SELECT o.id, t AS tag FROM sales.orders o JOIN t IN o.tags", want: "SELECT o.id, t AS tag FROM o JOIN t IN o.tags"},
		{name: "a string holding a parenthesis", body: `SELECT o.id FROM sales.orders o WHERE o.note = "a) b"`, want: `SELECT o.id FROM o WHERE o.note = "a) b"`},
		{name: "a subquery", body: "SELECT o.id FROM sales.orders o WHERE EXISTS (SELECT VALUE t FROM t IN o.tags)", want: "SELECT o.id FROM o WHERE EXISTS (SELECT VALUE t FROM t IN o.tags)"},
		{name: "a lone cross apply", body: "SELECT o.id, l.sku FROM sales.orders o CROSS APPLY l IN o.lines", want: "SELECT o.id, l.sku FROM o JOIN l IN o.lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan("WITH x AS (" + tt.body + ") SELECT x.id, cu.name FROM x JOIN sales.customers cu ON x.id = cu.id")

			require.NoError(t, err)
			require.Len(t, plan.Leaves, 2)
			assert.Equal(t, adapter.Query{Text: tt.want, Scope: []string{"sales", "orders"}}, plan.Leaves[0].Query)
			assert.Equal(t, "x", plan.Leaves[0].Name)
		})
	}
}

func TestTheColumnsOfEveryCTEComeFromItsSelectList(t *testing.T) {
	tests := []struct {
		name string
		list string
		want []string
	}{
		{name: "AS", list: "ROUND(o.total) AS total, o.id AS orderId", want: []string{"total", "orderId"}},
		{name: "a bare trailing name", list: "ROUND(o.total) total", want: []string{"total"}},
		{name: "a path", list: "o.id, o.shipTo.city", want: []string{"id", "city"}},
		{name: "top and distinct", list: "DISTINCT TOP 5 o.id", want: []string{"id"}},
		{name: "star", list: "*"},
		{name: "value", list: "VALUE o.id"},
		{name: "an unnamed expression", list: "o.id, o.total * 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan("WITH x AS (SELECT " + tt.list + " FROM sales.orders o) SELECT * FROM x")

			require.NoError(t, err)
			assert.Equal(t, tt.want, plan.Leaves[0].Fields)
		})
	}
}

func TestJoiningCTEsReadsEachAsAnInputWithItsFields(t *testing.T) {
	plan, err := query.BuildPlan(westAndBig)

	require.NoError(t, err)
	assert.Equal(t, []query.Leaf{
		{
			Alias:  "cu",
			Query:  adapter.Query{Text: `SELECT cu.id, cu.name FROM cu WHERE cu.region = "west"`, Scope: []string{"sales", "customers"}},
			Name:   "west",
			Fields: []string{"id", "name"},
		},
		{
			Alias: "o",
			Query: adapter.Query{
				Text:  "SELECT o.id, o.customerId, ROUND(o.total) AS total\n              FROM o WHERE o.total > 100",
				Scope: []string{"sales", "orders"},
			},
			Name:   "big",
			Fields: []string{"id", "customerId", "total"},
		},
	}, plan.Leaves)
	assert.Equal(t, []query.Source{
		{Alias: "big", Rows: &query.Scan{Leaf: 1}, Fields: []string{"id", "customerId", "total"}},
		{Alias: "west", Rows: &query.Scan{Leaf: 0}, Fields: []string{"id", "name"}},
	}, joinOf(t, plan).Inputs)
	assert.Equal(t, []string{"sales", "customers"}, plan.Scope(), "the first container the text names")
}

func TestReadingOneCTETwiceSharesOneMaterialize(t *testing.T) {
	plan, err := query.BuildPlan(staff)

	require.NoError(t, err)
	inputs := joinOf(t, plan).Inputs
	shared, ok := inputs[0].Rows.(*query.Materialize)
	require.True(t, ok)
	assert.Same(t, shared, inputs[1].Rows)
	assert.Equal(t, &query.Materialize{Name: "staff", Input: &query.Scan{Leaf: 0}}, shared)
	assert.Len(t, plan.Leaves, 1)
}

func TestUnreadCTEsArePlannedButNotInTheTree(t *testing.T) {
	plan, err := query.BuildPlan("WITH spare AS (SELECT * FROM hr.employees e) SELECT * FROM sales.orders o")

	require.NoError(t, err)
	assert.Equal(t, &query.Scan{Leaf: 1}, plan.Root)
	assert.Equal(t, "spare", plan.Leaves[0].Name)
	assert.Equal(t, "SELECT * FROM o", plan.Leaves[1].Query.Text)
}

func TestReadingWholeCTEsPassesTheirBodiesThrough(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "by name", input: "WITH recent AS (SELECT TOP 50 * FROM sales.orders o ORDER BY o._ts DESC) SELECT * FROM recent", want: "SELECT TOP 50 * FROM o ORDER BY o._ts DESC"},
		{name: "under an alias", input: "WITH v AS (SELECT VALUE o.id FROM sales.orders o) SELECT * FROM v AS r", want: "SELECT VALUE o.id FROM o"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)

			require.NoError(t, err)
			assert.False(t, plan.Simulated())
			assert.Equal(t, &query.Scan{Leaf: 0}, plan.Root)
			assert.Equal(t, tt.want, plan.Leaves[0].Query.Text)
		})
	}
}

func TestABareNameIsTheScopedContainerInsideTheCTEThatDeclaresIt(t *testing.T) {
	plan, err := query.BuildPlan("WITH c AS (SELECT * FROM c WHERE c.open) SELECT * FROM c")

	require.NoError(t, err)
	require.Len(t, plan.Leaves, 1)
	assert.Equal(t, "SELECT * FROM c WHERE c.open", plan.Leaves[0].Query.Text)
	assert.True(t, plan.NeedsDefaultScope())
	assert.Equal(t, []string{"sales", "orders"}, plan.WithDefaultScope([]string{"sales", "orders"}).Scope())
}

func TestComposedCTEsFlattenAJoinOverAUnion(t *testing.T) {
	plan, err := query.BuildPlan(composed)

	require.NoError(t, err)
	named, ok := joinOf(t, plan).Inputs[0].Rows.(*query.Flatten)
	require.True(t, ok)
	assert.Equal(t, "named", named.Name)
	assert.Equal(t, []string{"orderId", "customer", "sku"}, joinOf(t, plan).Inputs[0].Fields)
	assert.Equal(t, &query.Union{Leaves: []int{0, 1}}, named.Input.Inputs[0].Rows)
	var labels []string
	for _, leaf := range plan.Leaves {
		labels = append(labels, leaf.Name+" "+leaf.Label())
	}
	assert.Equal(t, []string{
		"everything sales.orders", "everything sales.archive", "named sales.customers", " sales.products",
	}, labels)
}

func TestOneCTEMayRenameAnEarlierOne(t *testing.T) {
	plan, err := query.BuildPlan("WITH big AS (SELECT o.id FROM sales.orders o), ids AS (SELECT b.id AS n FROM big b) " +
		"SELECT ids.n, cu.name FROM ids JOIN sales.customers cu ON ids.n = cu.id")

	require.NoError(t, err)
	ids, ok := joinOf(t, plan).Inputs[0].Rows.(*query.Flatten)
	require.True(t, ok)
	assert.Empty(t, ids.Input.Steps)
	assert.Equal(t, []query.JoinColumn{{Alias: "b", Field: "id", As: "n"}}, ids.Input.Columns)
}
