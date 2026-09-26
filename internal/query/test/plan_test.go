package query_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

func TestASingleContainerQueryPassesThrough(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		scope     []string
		rewritten string
	}{
		{
			name:      "as alias",
			input:     "SELECT * FROM mydb.orders AS c WHERE c.total > 5",
			scope:     []string{"mydb", "orders"},
			rewritten: "SELECT * FROM c WHERE c.total > 5",
		},
		{
			name:      "bare alias",
			input:     "SELECT * FROM mydb.orders o",
			scope:     []string{"mydb", "orders"},
			rewritten: "SELECT * FROM o",
		},
		{
			name:      "already scoped",
			input:     "SELECT * FROM c",
			rewritten: "SELECT * FROM c",
		},
		{
			name:      "join over property path",
			input:     "SELECT t.name FROM c JOIN t IN c.tags",
			rewritten: "SELECT t.name FROM c JOIN t IN c.tags",
		},
		{
			name:      "container joined with its own property path",
			input:     "SELECT t FROM mydb.orders o JOIN t IN o.tags",
			scope:     []string{"mydb", "orders"},
			rewritten: "SELECT t FROM o JOIN t IN o.tags",
		},
		{
			name:      "dotted pair inside string literal",
			input:     `SELECT * FROM c WHERE c.note = "FROM a.b"`,
			rewritten: `SELECT * FROM c WHERE c.note = "FROM a.b"`,
		},
		{
			name:      "case-insensitive keywords",
			input:     "select * from MyDb.Orders as c",
			scope:     []string{"MyDb", "Orders"},
			rewritten: "select * from c",
		},
		{
			name:      "value projection",
			input:     "SELECT VALUE c.id FROM mydb.orders c",
			scope:     []string{"mydb", "orders"},
			rewritten: "SELECT VALUE c.id FROM c",
		},
		{
			name:      "no alias defaults to c",
			input:     "SELECT * FROM mydb.orders WHERE 1 = 1",
			scope:     []string{"mydb", "orders"},
			rewritten: "SELECT * FROM c WHERE 1 = 1",
		},
		{
			name:      "deep property path untouched",
			input:     "SELECT * FROM a.b.c.d",
			rewritten: "SELECT * FROM a.b.c.d",
		},
		{
			name: "empty input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)

			require.NoError(t, err)
			require.Equal(t, query.PassThrough, plan.Merge)
			require.False(t, plan.Simulated())
			require.Len(t, plan.Leaves, 1)
			assert.Equal(t, tt.rewritten, plan.Leaves[0].Query.Text)
			assert.Equal(t, tt.scope, plan.Scope())
		})
	}
}

func TestAnUnscopedPlanTakesTheDefaultScope(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM c")
	require.NoError(t, err)

	scoped := plan.WithDefaultScope([]string{"sales", "orders"})

	assert.Equal(t, []string{"sales", "orders"}, scoped.Scope())
	assert.Empty(t, plan.Scope(), "the plan it was derived from is left alone")
}

func TestAScopeWrittenInTheQueryOutranksTheDefault(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM telemetry.events e")
	require.NoError(t, err)

	scoped := plan.WithDefaultScope([]string{"sales", "orders"})

	assert.Equal(t, []string{"telemetry", "events"}, scoped.Scope())
}

func TestNeedsDefaultScope(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "bare source", input: "SELECT * FROM c", want: true},
		{name: "named container", input: "SELECT * FROM sales.orders c"},
		{name: "container list", input: "SELECT * FROM sales.orders, sales.archive AS c"},
		{name: "join of named containers", input: "SELECT o.id FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)
			require.NoError(t, err)

			assert.Equal(t, tt.want, plan.NeedsDefaultScope(), "NeedsDefaultScope(%q)", tt.input)
		})
	}
}

func TestAContainerListPlansAUnionAll(t *testing.T) {
	tests := []struct {
		name  string
		input string
		text  string
	}{
		{
			name:  "one alias after the list",
			input: `SELECT * FROM sales.orders, sales.archive AS c WHERE c.status = "open"`,
			text:  `SELECT * FROM c WHERE c.status = "open"`,
		},
		{
			name:  "no alias defaults to c",
			input: "SELECT * FROM sales.orders, sales.archive",
			text:  "SELECT * FROM c",
		},
		{
			name:  "the same alias on every container",
			input: "SELECT o.id FROM sales.orders o, sales.archive o",
			text:  "SELECT o.id FROM o",
		},
		{
			name:  "property join after the list",
			input: "SELECT t FROM sales.orders, sales.archive AS c JOIN t IN c.tags",
			text:  "SELECT t FROM c JOIN t IN c.tags",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)

			require.NoError(t, err)
			require.Equal(t, query.UnionAll, plan.Merge)
			require.True(t, plan.Simulated())
			require.Len(t, plan.Leaves, 2)
			assert.Equal(t, adapter.Query{Text: tt.text, Scope: []string{"sales", "orders"}}, plan.Leaves[0].Query)
			assert.Equal(t, adapter.Query{Text: tt.text, Scope: []string{"sales", "archive"}}, plan.Leaves[1].Query)
		})
	}
}

func TestAnEqualityJoinPlansAHashJoin(t *testing.T) {
	plan, err := query.BuildPlan(`SELECT o.id, o.total, cu.name
FROM sales.orders AS o
JOIN sales.customers AS cu ON o.customerId = cu.id`)

	require.NoError(t, err)
	require.Equal(t, query.HashJoin, plan.Merge)
	require.Len(t, plan.Leaves, 2)
	assert.Equal(t, query.Leaf{
		Alias: "o",
		Query: adapter.Query{Text: "SELECT * FROM o", Scope: []string{"sales", "orders"}},
	}, plan.Leaves[0])
	assert.Equal(t, query.Leaf{
		Alias: "cu",
		Query: adapter.Query{Text: "SELECT * FROM cu", Scope: []string{"sales", "customers"}},
	}, plan.Leaves[1])
	assert.Equal(t, query.Join{
		Steps: []query.JoinStep{{Left: 0, LeftKey: []string{"customerId"}, RightKey: []string{"id"}}},
		Columns: []query.JoinColumn{
			{Side: 0, Field: "id"},
			{Side: 0, Field: "total"},
			{Side: 1, Field: "name"},
		},
	}, plan.Join)
}

func TestAJoinColumnMayBeRenamed(t *testing.T) {
	plan, err := query.BuildPlan(
		"SELECT o.id, cu.name as newName, cu.id customer FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id")

	require.NoError(t, err)
	assert.Equal(t, []query.JoinColumn{
		{Side: 0, Field: "id"},
		{Side: 1, Field: "name", As: "newName"},
		{Side: 1, Field: "id", As: "customer"},
	}, plan.Join.Columns)
}

func TestJoinKeysFollowTheirSideHoweverOnIsWritten(t *testing.T) {
	plan, err := query.BuildPlan(
		"SELECT * FROM sales.orders o INNER JOIN sales.customers cu ON cu.profile.id = o.customerId")

	require.NoError(t, err)
	assert.Equal(t, []query.JoinStep{{Left: 0, LeftKey: []string{"customerId"}, RightKey: []string{"profile", "id"}}},
		plan.Join.Steps)
	assert.Empty(t, plan.Join.Columns, "SELECT * projects every column")
}

func TestAJoinSideWithNoAliasIsKnownByItsContainer(t *testing.T) {
	plan, err := query.BuildPlan(
		"SELECT * FROM sales.orders JOIN sales.customers ON orders.customerId = customers.id")

	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM orders", plan.Leaves[0].Query.Text)
	assert.Equal(t, "SELECT * FROM customers", plan.Leaves[1].Query.Text)
}

func TestJoinConditionsArePushedDownToTheSideTheyRead(t *testing.T) {
	tests := []struct {
		name  string
		where string
		left  string
		right string
	}{
		{
			name:  "one side only",
			where: `cu.region = "west"`,
			left:  "SELECT * FROM o",
			right: `SELECT * FROM cu WHERE (cu.region = "west")`,
		},
		{
			name:  "a conjunct for each side",
			where: `o.total > 5 AND cu.region = "west" AND o.open`,
			left:  "SELECT * FROM o WHERE (o.total > 5) AND (o.open)",
			right: `SELECT * FROM cu WHERE (cu.region = "west")`,
		},
		{
			name:  "OR keeps the clause whole",
			where: "o.total > 5 AND o.open OR o.rush",
			left:  "SELECT * FROM o WHERE (o.total > 5 AND o.open OR o.rush)",
			right: "SELECT * FROM cu",
		},
		{
			name:  "BETWEEN keeps the clause whole",
			where: "o.total BETWEEN 1 AND 5",
			left:  "SELECT * FROM o WHERE (o.total BETWEEN 1 AND 5)",
			right: "SELECT * FROM cu",
		},
		{
			name:  "AND inside parentheses is not a cut",
			where: "(o.total > 5 AND o.open) AND cu.vip",
			left:  "SELECT * FROM o WHERE ((o.total > 5 AND o.open))",
			right: "SELECT * FROM cu WHERE (cu.vip)",
		},
		{
			name:  "a condition reading neither side filters both",
			where: "1 = 1",
			left:  "SELECT * FROM o WHERE (1 = 1)",
			right: "SELECT * FROM cu WHERE (1 = 1)",
		},
		{
			name:  "an alias after a dot is a property",
			where: "o.cu = 1",
			left:  "SELECT * FROM o WHERE (o.cu = 1)",
			right: "SELECT * FROM cu",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(
				"SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id WHERE " + tt.where)

			require.NoError(t, err)
			assert.Equal(t, tt.left, plan.Leaves[0].Query.Text)
			assert.Equal(t, tt.right, plan.Leaves[1].Query.Text)
			assert.Equal(t, tt.left != "SELECT * FROM o", plan.Leaves[0].Filtered)
			assert.Equal(t, tt.right != "SELECT * FROM cu", plan.Leaves[1].Filtered)
		})
	}
}

const (
	starJoin = `SELECT o.id, cu.name, p.name AS product
FROM sales.orders o
JOIN sales.customers cu ON o.customerId = cu.id
JOIN sales.products p ON o.sku = p.id
WHERE cu.region = "west"`
	chainJoin = `SELECT a.message, e.kind, d.site
FROM telemetry.alerts a
JOIN telemetry.events e ON a.deviceId = e.deviceId
JOIN telemetry.devices d ON e.deviceId = d.id
WHERE a.open = true AND e.kind = "pressure"`
)

func TestALookupStarAttachesEverySideToTheFirst(t *testing.T) {
	plan, err := query.BuildPlan(starJoin)

	require.NoError(t, err)
	require.Equal(t, query.HashJoin, plan.Merge)
	assert.Equal(t, []query.Leaf{
		{Alias: "o", Query: adapter.Query{Text: "SELECT * FROM o", Scope: []string{"sales", "orders"}}},
		{
			Alias:    "cu",
			Query:    adapter.Query{Text: `SELECT * FROM cu WHERE (cu.region = "west")`, Scope: []string{"sales", "customers"}},
			Filtered: true,
		},
		{Alias: "p", Query: adapter.Query{Text: "SELECT * FROM p", Scope: []string{"sales", "products"}}},
	}, plan.Leaves)
	assert.Equal(t, []query.JoinStep{
		{Left: 0, LeftKey: []string{"customerId"}, RightKey: []string{"id"}},
		{Left: 0, LeftKey: []string{"sku"}, RightKey: []string{"id"}},
	}, plan.Join.Steps)
}

func TestAChainAttachesEachSideToTheOneBeforeIt(t *testing.T) {
	plan, err := query.BuildPlan(chainJoin)

	require.NoError(t, err)
	assert.Equal(t, query.JoinStep{Left: 1, LeftKey: []string{"deviceId"}, RightKey: []string{"id"}}, plan.Join.Steps[1])
	assert.Equal(t, []bool{true, true, false}, filtered(plan))
}

func TestAnOnWrittenNewSideFirstIsTheSameStep(t *testing.T) {
	newFirst, err := query.BuildPlan(strings.Replace(starJoin, "o.sku = p.id", "p.id = o.sku", 1))
	require.NoError(t, err)
	earlierFirst, err := query.BuildPlan(starJoin)
	require.NoError(t, err)

	assert.Equal(t, earlierFirst.Join.Steps, newFirst.Join.Steps)
}

func TestFourContainersMixingStarAndChainKeepEachLeftAsWritten(t *testing.T) {
	plan, err := query.BuildPlan(`SELECT * FROM a.w w
JOIN a.x x ON w.k = x.k
JOIN a.y y ON x.k = y.k
INNER JOIN a.z z ON z.k = w.k`)

	require.NoError(t, err)
	var lefts []int
	for _, step := range plan.Join.Steps {
		lefts = append(lefts, step.Left)
	}
	assert.Equal(t, []int{0, 1, 0}, lefts)
}

func TestASideWithNoAliasInTheMiddleOfAChainIsKnownByItsContainer(t *testing.T) {
	plan, err := query.BuildPlan(
		"SELECT * FROM sales.orders o JOIN sales.customers ON o.customerId = customers.id " +
			"JOIN sales.regions r ON customers.region = r.id")

	require.NoError(t, err)
	assert.Equal(t, "customers", plan.Leaves[1].Alias)
	assert.Equal(t, 1, plan.Join.Steps[1].Left)
}

func TestOneContainerTwiceIsTwoLeavesOfOneScope(t *testing.T) {
	plan, err := query.BuildPlan("SELECT e.name, m.name AS manager FROM hr.employees e JOIN hr.employees m ON e.managerId = m.id")

	require.NoError(t, err)
	require.Len(t, plan.Leaves, 2)
	assert.Equal(t, plan.Leaves[0].Query.Scope, plan.Leaves[1].Query.Scope)
}

func TestJoinColumnsIndexTheSideTheyRead(t *testing.T) {
	plan, err := query.BuildPlan("SELECT p.name, o.id, cu.name FROM sales.orders o " +
		"JOIN sales.customers cu ON o.customerId = cu.id JOIN sales.products p ON o.sku = p.id")

	require.NoError(t, err)
	assert.Equal(t, []query.JoinColumn{
		{Side: 2, Field: "name"},
		{Side: 0, Field: "id"},
		{Side: 1, Field: "name"},
	}, plan.Join.Columns)
}

func TestAConditionReadingNoAliasFiltersEverySide(t *testing.T) {
	plan, err := query.BuildPlan(strings.Replace(starJoin, `cu.region = "west"`, "1 = 1", 1))

	require.NoError(t, err)
	assert.Equal(t, []bool{true, true, true}, filtered(plan))
}

func filtered(plan query.Plan) []bool {
	var filtered []bool
	for _, leaf := range plan.Leaves {
		filtered = append(filtered, leaf.Filtered)
	}
	return filtered
}

func TestMultiWayShapesThatCannotBeSimulatedAreRefusedByName(t *testing.T) {
	const pair = "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"
	tests := []struct {
		name  string
		input string
		shape string
	}{
		{name: "ON between two earlier sides", input: pair + " JOIN sales.products p ON o.sku = cu.sku", shape: "ON clause"},
		{name: "ON reading a side joined further right", input: pair + " JOIN sales.products p ON cu.x = r.id JOIN sales.regions r ON p.r = r.id", shape: "ON clause"},
		{name: "ON reading the new side twice", input: pair + " JOIN sales.products p ON p.sku = p.id", shape: "ON clause"},
		{name: "compound ON in the second step", input: pair + " JOIN sales.products p ON o.sku = p.id AND o.x = p.x", shape: "ON clause"},
		{name: "compound ON before a third side", input: pair + " AND o.x = cu.x JOIN sales.products p ON o.sku = p.id", shape: "ON clause"},
		{name: "left join as the third source", input: pair + " LEFT JOIN sales.products p ON o.sku = p.id", shape: "LEFT JOIN"},
		{name: "property join before the container joins", input: "SELECT * FROM sales.orders o JOIN l IN o.lines JOIN sales.customers cu ON o.customerId = cu.id", shape: "JOIN ... IN"},
		{name: "property join between the container joins", input: pair + " JOIN l IN o.lines JOIN sales.products p ON o.sku = p.id", shape: "JOIN ... IN"},
		{name: "property join after the container joins", input: pair + " JOIN l IN o.lines", shape: "JOIN ... IN"},
		{name: "inner property join after the container joins", input: pair + " INNER JOIN l IN o.lines", shape: "JOIN ... IN"},
		{name: "a list before a join", input: "SELECT * FROM a.b, c.d JOIN e.f ON b.x = f.y", shape: "a container list mixed with a join"},
		{name: "a list after a join", input: pair + ", sales.archive", shape: "a container list mixed with a join"},
		{name: "a list between joins", input: pair + ", sales.archive a JOIN sales.products p ON a.sku = p.id", shape: "a container list mixed with a join"},
		{name: "one container twice with no aliases", input: "SELECT * FROM hr.employees JOIN hr.employees ON employees.managerId = employees.id", shape: "share an alias"},
		{name: "a condition over two of three sides", input: pair + " JOIN sales.products p ON o.sku = p.id WHERE cu.region = p.region", shape: "more than one side"},
		{name: "order by after a three-way join", input: pair + " JOIN sales.products p ON o.sku = p.id ORDER BY o.id", shape: "ORDER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.BuildPlan(tt.input)

			require.ErrorIs(t, err, query.ErrUnsupported)
			assert.ErrorContains(t, err, tt.shape)
		})
	}
}

func TestShapesThatCannotBeSimulatedAreRefused(t *testing.T) {
	const join = "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"
	tests := []struct {
		name  string
		input string
	}{
		{name: "non-equality ON", input: "SELECT * FROM a.b x JOIN c.d y ON x.k > y.k"},
		{name: "compound ON", input: join + " AND o.region = cu.region"},
		{name: "ON over one side", input: "SELECT * FROM a.b x JOIN c.d y ON x.k = x.j"},
		{name: "ON against a literal", input: "SELECT * FROM a.b x JOIN c.d y ON x.k = 5"},
		{name: "join without ON", input: "SELECT * FROM a.b x JOIN c.d y"},
		{name: "left join", input: "SELECT * FROM a.b x LEFT JOIN c.d y ON x.k = y.k"},
		{name: "left outer join with no alias", input: "SELECT * FROM a.b LEFT OUTER JOIN c.d ON b.k = d.k"},
		{name: "cross join", input: "SELECT * FROM a.b x CROSS JOIN c.d y"},
		{name: "cross-container subquery", input: "SELECT * FROM a.b x WHERE EXISTS (SELECT VALUE y FROM c.d y)"},
		{name: "condition over both sides", input: join + " WHERE o.total > cu.limit"},
		{name: "empty condition", input: join + " WHERE o.open AND"},
		{name: "order by", input: join + " WHERE o.open ORDER BY o.total"},
		{name: "order by with no where", input: join + " ORDER BY o.total"},
		{name: "computed projection", input: "SELECT COUNT(1) FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "nested projection", input: "SELECT x.address.city FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "projection of an unknown alias", input: "SELECT z.id FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "AS with no name", input: "SELECT x.id AS FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "two columns named alike", input: "SELECT x.id AS n, y.id AS n FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "top", input: "SELECT TOP 5 x.id FROM a.b x JOIN c.d y ON x.k = y.k"},
		{name: "sides sharing an alias", input: "SELECT * FROM a.b x JOIN c.d x ON x.k = x.k"},
		{name: "property join between the sides", input: "SELECT * FROM a.b x JOIN t IN x.tags JOIN c.d y ON x.k = y.k"},
		{name: "list with two aliases", input: "SELECT * FROM sales.orders o, sales.customers c"},
		{name: "list mixed with a plain source", input: "SELECT * FROM c, sales.orders, sales.archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.BuildPlan(tt.input)

			require.ErrorIs(t, err, query.ErrUnsupported)
		})
	}
}

func TestTwoStatementsAreNotMistakenForASubquery(t *testing.T) {
	_, err := query.BuildPlan("SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id " +
		"and SELECT * FROM sales.orders, sales.archive")

	require.ErrorIs(t, err, query.ErrUnsupported)
	assert.ErrorContains(t, err, "more than one statement")
}

// plannerSeeds is the fuzz corpus every parser entry point shares.
var plannerSeeds = []string{
	"SELECT * FROM mydb.orders AS c WHERE c.total > 5",
	"SELECT * FROM c",
	"SELECT t.name FROM c JOIN t IN c.tags",
	`SELECT * FROM c WHERE c.note = "FROM a.b"`,
	"select * from MyDb.Orders as c",
	"FROM",
	"FROM a.",
	"FROM a.b AS",
	"FROM a.b JOIN c.d ON",
	"FROM a.b x JOIN c.d y ON x.k = y.k WHERE",
	"'unterminated",
	`"also unterminated \`,
	"SELECT * FROM a.b, x.y",
	"SELECT x.k, FROM a.b x JOIN c.d y ON x.k = y.k WHERE (x.a AND",
	"SELECT * FROM (SELECT * FROM inner.things) outer",
	starJoin,
	chainJoin,
	"..,,..''\"\"[[]]",
}

func FuzzBuildPlan(f *testing.F) {
	for _, seed := range plannerSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		plan, err := query.BuildPlan(input)
		if err != nil {
			return
		}
		require.NotEmpty(t, plan.Leaves)
		if !plan.Simulated() && len(plan.Scope()) == 0 {
			assert.Equal(t, input, plan.Leaves[0].Query.Text)
		}
	})
}

// A subquery's alias is a name the statement reads through, never an alias
// that makes a db.container path a property instead.
func TestASubqueryAliasedLikeADatabaseLeavesTheScopeAlone(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM sales.orders o WHERE EXISTS(SELECT VALUE 1 FROM (SELECT * FROM c) sales)")

	require.NoError(t, err)
	require.Equal(t, query.PassThrough, plan.Merge)
	require.Equal(t, []string{"sales", "orders"}, plan.Scope())
}

func TestASubqueryAliasedLikeADatabaseLeavesAJoinPlanned(t *testing.T) {
	plan, err := query.BuildPlan("SELECT o.id, cu.name FROM sales.orders o JOIN sales.customers cu ON o.cid = cu.id " +
		"WHERE EXISTS(SELECT VALUE 1 FROM (SELECT * FROM c) sales)")

	require.NoError(t, err)
	require.Equal(t, query.HashJoin, plan.Merge)
	require.Len(t, plan.Leaves, 2)
}

func TestSourcesAfterASubqueryAreStillParsed(t *testing.T) {
	_, err := query.BuildPlan("SELECT * FROM (SELECT * FROM sales.orders) x JOIN sales.customers cu ON x.cid = cu.id")

	require.ErrorIs(t, err, query.ErrUnsupported, "the join after the subquery is seen, and refused")
}

func TestAPropertyJoinAfterASubqueryPassesThrough(t *testing.T) {
	text := "SELECT * FROM (SELECT * FROM c) x JOIN t IN x.tags WHERE t.a = 1"

	plan, err := query.BuildPlan(text)

	require.NoError(t, err)
	require.Equal(t, query.PassThrough, plan.Merge)
	require.Equal(t, text, plan.Leaves[0].Query.Text)
}

func TestASubqueryJoinAliasedLikeADatabaseLeavesTheScopeAlone(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM sales.orders o JOIN (SELECT VALUE t FROM t IN o.tags) AS sales")

	require.NoError(t, err)
	require.Equal(t, []string{"sales", "orders"}, plan.Scope())
}

// A path rooted at a subquery's alias reads the subquery's items: it is no
// db.container, and the query passes through as written.
func TestAJoinUnderASubqueryAliasIsNoContainer(t *testing.T) {
	tests := []struct {
		name  string
		query string
		scope []string
		text  string
	}{
		{name: "a FROM subquery", query: "SELECT * FROM (SELECT * FROM c) x JOIN x.items i"},
		{name: "a JOIN subquery", query: "SELECT * FROM c JOIN (SELECT VALUE t FROM t IN c.tags) x JOIN x.parts p"},
		{name: "a subquery declared inside another", query: "SELECT * FROM (SELECT * FROM (SELECT * FROM c) y) x JOIN y.items i"},
		{name: "a subquery named like a database", query: "SELECT * FROM (SELECT * FROM c) sales JOIN sales.orders s"},
		{name: "a join under a join under a subquery", query: "SELECT * FROM (SELECT * FROM c) x JOIN x.items i JOIN i.parts p"},
		{
			name:  "a subquery beside a container",
			query: "SELECT * FROM sales.orders o JOIN (SELECT VALUE t FROM t IN o.tags) x JOIN x.parts p",
			scope: []string{"sales", "orders"},
			text:  "SELECT * FROM o JOIN (SELECT VALUE t FROM t IN o.tags) x JOIN x.parts p",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.query)

			require.NoError(t, err)
			require.Equal(t, query.PassThrough, plan.Merge)
			want := tt.text
			if want == "" {
				want = tt.query
			}
			require.Equal(t, want, plan.Leaves[0].Query.Text)
			require.Equal(t, tt.scope, plan.Scope())
		})
	}
}

func TestASubqueryIsNoSecondStatement(t *testing.T) {
	_, err := query.BuildPlan("SELECT * FROM sales.orders o JOIN (SELECT VALUE t FROM t IN o.tags) x " +
		"JOIN sales.customers cu ON o.cid = cu.id")

	require.ErrorIs(t, err, query.ErrUnsupported)
	require.NotContains(t, err.Error(), "more than one statement")
}

func TestClausesStillFollowAQueryWithASubquery(t *testing.T) {
	text := "SELECT * FROM sales.orders o JOIN (SELECT VALUE t FROM t IN o.tags) x WHERE 1=1 "

	got := query.Context(text, len(text))

	require.Subset(t, got.Keywords, []string{"GROUP BY", "ORDER BY", "OFFSET"})
}
