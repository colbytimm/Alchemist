package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

func TestSpansKnowTheJoinTypesApplyAndCTEs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []spanText
	}{
		{
			name:  "a CTE, declared and read, and an outer join with an apply",
			input: "WITH w AS (SELECT cu.id FROM sales.customers cu) SELECT w.id, l.sku FROM w LEFT OUTER JOIN sales.orders o ON w.id = o.cid OUTER APPLY l IN o.lines",
			want: []spanText{
				keyword("WITH"), alias("w"), keyword("AS"), keyword("SELECT"), alias("cu"), keyword("FROM"), alias("cu"),
				keyword("SELECT"), alias("w"), alias("l"), keyword("FROM"), alias("w"), keyword("LEFT"), keyword("OUTER"),
				keyword("JOIN"), alias("o"), keyword("ON"), alias("w"), alias("o"), keyword("OUTER"), keyword("APPLY"),
				alias("l"), operator("IN"), alias("o"),
			},
		},
		{
			name:  "right, full and cross, and the absent-side test",
			input: "SELECT * FROM a.b x RIGHT JOIN c.d y ON x.k = y.k CROSS APPLY t IN y.tags WHERE NOT IS_DEFINED(x)",
			want: []spanText{
				keyword("SELECT"), keyword("FROM"), alias("x"), keyword("RIGHT"), keyword("JOIN"), alias("y"), keyword("ON"),
				alias("x"), alias("y"), keyword("CROSS"), keyword("APPLY"), alias("t"), operator("IN"), alias("y"),
				keyword("WHERE"), operator("NOT"), function("IS_DEFINED"),
			},
		},
		{
			name:  "a full join and a cross join",
			input: "SELECT * FROM a.b x FULL JOIN c.d y ON x.k = y.k CROSS JOIN e.f z",
			want: []spanText{
				keyword("SELECT"), keyword("FROM"), alias("x"), keyword("FULL"), keyword("JOIN"), alias("y"), keyword("ON"),
				alias("x"), alias("y"), keyword("CROSS"), keyword("JOIN"), alias("z"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, wordTexts(tt.input))
		})
	}
}

func TestNoJoinTypeOrCTEFormIsFlagged(t *testing.T) {
	for _, input := range []string{
		leftJoin + ` WHERE NOT IS_DEFINED(cu) AND o.status = "open"`,
		rightJoin,
		fullJoin + " AND cu.vip",
		crossQuery,
		"SELECT o.id, l.sku, t FROM sales.orders o OUTER APPLY l IN o.lines CROSS APPLY t IN o.tags",
		"SELECT o.id, l.quantity, p.name FROM sales.orders o CROSS APPLY l IN o.lines JOIN sales.products p ON l.sku = p.id",
		westAndBig,
		staff,
		composed,
		"WITH recent AS (SELECT TOP 50 * FROM sales.orders o ORDER BY o._ts DESC) SELECT * FROM recent",
		"WITH c AS (SELECT * FROM c WHERE c.open) SELECT * FROM c",
		"WITH w AS (SELECT cu.id FROM sales.customers cu) SELECT w.id FROM w",
		"WITH w AS (SELECT cu.id FROM sales.customers cu)",
	} {
		assert.Empty(t, diagnose(input), input)
	}
}

func TestTheJoinAndCTEFormsThePlannerRefusesByNameAreFlagged(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []flagged
	}{
		{name: "NATURAL JOIN", input: "SELECT * FROM a.b x NATURAL JOIN c.d y", want: []flagged{{"NATURAL", "NATURAL JOIN is not supported: join ON an equality"}}},
		{name: "WITH RECURSIVE", input: "WITH RECURSIVE r AS (SELECT * FROM a.b) SELECT * FROM r", want: []flagged{{"RECURSIVE", "a recursive CTE is not supported"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, diagnose(tt.input))
		})
	}
}

func TestAWithSelectIsAQueryForThePlanner(t *testing.T) {
	assert.False(t, query.IsMutation(westAndBig))
	assert.False(t, query.IsMutation(staff))
	_, err := query.BuildPlan(westAndBig)
	require.NoError(t, err)
}

func TestMultibyteNamesKeepTheirBytesThroughTheRewrites(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "a CTE and an alias named in other scripts",
			input: "WITH 西 AS (SELECT o.id FROM sales.orders o WHERE o.名 = 1) SELECT 西.id, çu.name FROM 西 JOIN sales.customers çu ON 西.id = çu.id",
			want:  []string{"SELECT o.id FROM o WHERE o.名 = 1", "SELECT * FROM çu"},
		},
		{
			name:  "an outer join and a pushed-down condition",
			input: "SELECT é.id FROM sales.orders é LEFT JOIN sales.customers ç ON é.cid = ç.id WHERE é.名 = 'ü'",
			want:  []string{"SELECT * FROM é WHERE (é.名 = 'ü')", "SELECT * FROM ç"},
		},
		{
			name:  "a lone cross apply, rewritten, beside a no-break space",
			input: "SELECT ö.id FROM sales.orders ö CROSS APPLY ł IN ö.lines",
			want:  []string{"SELECT ö.id FROM ö JOIN ł IN ö.lines"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := query.BuildPlan(tt.input)
			require.NoError(t, err)

			var got []string
			for _, leaf := range plan.Leaves {
				got = append(got, leaf.Query.Text)
			}
			assert.Equal(t, tt.want, got)
			assert.Empty(t, diagnose(tt.input))
		})
	}
}

func TestAJoinModifierAfterAnAliasIsNoMisspelledClause(t *testing.T) {
	for _, input := range []string{
		"SELECT * FROM sales.orders o LEFT JOIN sales.customers cu ON o.cid = cu.id",
		"SELECT * FROM sales.orders o RIGHT OUTER JOIN sales.customers cu ON o.cid = cu.id",
		"SELECT * FROM sales.orders o FULL JOIN sales.customers cu ON o.cid = cu.id",
		"SELECT * FROM sales.orders o INNER JOIN sales.customers cu ON o.cid = cu.id",
		"SELECT * FROM sales.orders o CROSS JOIN sales.customers cu",
		"SELECT * FROM sales.orders o CROSS APPLY l IN o.lines",
		"SELECT * FROM sales.orders o OUTER APPLY l IN o.lines",
	} {
		assert.Empty(t, diagnose(input), input)
	}
}

func TestAnAliasedCTEReadDeclaresItsAlias(t *testing.T) {
	input := "WITH big AS (SELECT o.id FROM sales.orders o) SELECT b.id FROM big b JOIN sales.customers cu ON b.id = cu.id"

	assert.Empty(t, diagnose(input))
	assert.Equal(t, []spanText{
		keyword("WITH"), alias("big"), keyword("AS"), keyword("SELECT"), alias("o"), keyword("FROM"), alias("o"),
		keyword("SELECT"), alias("b"), keyword("FROM"), alias("b"), keyword("JOIN"), alias("cu"), keyword("ON"),
		alias("b"), alias("cu"),
	}, wordTexts(input))
}
