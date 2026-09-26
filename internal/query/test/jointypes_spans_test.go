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
