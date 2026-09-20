package query_test

import (
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
		LeftKey:  []string{"customerId"},
		RightKey: []string{"id"},
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
	assert.Equal(t, []string{"customerId"}, plan.Join.LeftKey)
	assert.Equal(t, []string{"profile", "id"}, plan.Join.RightKey)
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

func TestShapesThatCannotBeSimulatedAreRefused(t *testing.T) {
	const join = "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"
	tests := []struct {
		name  string
		input string
	}{
		{name: "three containers with a join", input: "SELECT * FROM a.b, c.d JOIN e.f ON b.x = f.y"},
		{name: "three-way join", input: join + " JOIN sales.regions r ON cu.region = r.id"},
		{name: "join followed by a list", input: join + ", sales.archive"},
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

func FuzzBuildPlan(f *testing.F) {
	for _, seed := range []string{
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
		"..,,..''\"\"[[]]",
	} {
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
