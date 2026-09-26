package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/query"
)

const withWest = "WITH west AS (SELECT cu.id, cu.name FROM sales.customers cu) "

func TestContextOffersTheJoinTypesAndApply(t *testing.T) {
	afterSources := []string{"WHERE", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "OUTER"}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "after LEFT", query: "SELECT * FROM sales.orders o LEFT |", want: []string{"OUTER", "JOIN"}},
		{name: "after LEFT OUTER", query: "SELECT * FROM sales.orders o LEFT OUTER |", want: []string{"JOIN"}},
		{name: "after FULL", query: "SELECT * FROM sales.orders o FULL |", want: []string{"OUTER", "JOIN"}},
		{name: "after CROSS", query: "SELECT * FROM sales.orders o CROSS |", want: []string{"JOIN", "APPLY"}},
		{name: "after OUTER alone", query: "SELECT * FROM sales.orders o OUTER |", want: []string{"APPLY"}},
		{name: "after the alias of an APPLY", query: "SELECT * FROM sales.orders o OUTER APPLY l |", want: []string{"IN"}},
		{name: "after a CROSS JOIN source, which takes no ON", query: "SELECT * FROM sales.orders o CROSS JOIN sales.customers cu |", want: afterSources},
		{name: "after an outer join's ON", query: leftList + " |", want: afterSources},
		{name: "after WITH and a name", query: "WITH west |", want: []string{"AS"}},
		{name: "at the start of a CTE body", query: "WITH west AS (|", want: []string{"SELECT"}},
		{name: "after a CTE", query: withWest + "|", want: []string{"SELECT"}},
		{name: "inside a CTE body", query: "WITH west AS (SELECT * FROM sales.customers cu |)", want: append(afterSources, "GROUP BY", "ORDER BY", "OFFSET")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)

			assert.Equal(t, query.CompleteKeyword, got.Kind)
			assert.Equal(t, tt.want, got.Keywords)
		})
	}
}

func TestContextOffersNothingInAWithHeader(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "the name of a CTE", query: "WITH |"},
		{name: "after AS", query: "WITH west AS |"},
		{name: "the name of a second CTE", query: withWest + ", |"},
		{name: "the alias of an APPLY", query: "SELECT * FROM sales.orders o CROSS APPLY |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, query.CompleteNothing, contextAt(t, tt.query).Kind)
		})
	}
}

func TestContextOffersTheCTEsDeclaredBeforeASource(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "in the main query", query: withWest + ", big AS (SELECT o.id FROM sales.orders o) SELECT * FROM |", want: []string{"west", "big"}},
		{name: "after JOIN", query: withWest + "SELECT * FROM sales.orders o LEFT JOIN |", want: []string{"west"}},
		{name: "in a later CTE", query: withWest + ", big AS (SELECT * FROM |", want: []string{"west"}},
		{name: "in the first CTE", query: "WITH west AS (SELECT * FROM |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, contextAt(t, tt.query).CTEs)
		})
	}
}

func TestContextCompletesACTEsFieldsFromItsSelectList(t *testing.T) {
	got := contextAt(t, withWest+"SELECT w.| FROM west w JOIN sales.orders o ON w.id = o.customerId")

	assert.Equal(t, query.CompleteField, got.Kind)
	assert.Equal(t, []query.Alias{{Name: "w", CTE: true, Fields: []string{"id", "name"}}}, got.Aliases)
	assert.True(t, got.TopLevel)
	assert.Empty(t, got.WithDefaultScope(orders).Aliases[0].Scopes, "a CTE is no container of the catalog's")
}

func TestContextOffersOnlyPreservedAliasesInTheWhereOfAnOuterJoin(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "left", query: leftList + " WHERE |", want: []string{"o"}},
		{name: "right", query: "SELECT * FROM sales.orders o RIGHT JOIN sales.customers cu ON o.customerId = cu.id WHERE |", want: []string{"cu"}},
		{name: "full", query: fullList + " WHERE |"},
		{name: "an apply", query: "SELECT * FROM sales.orders o OUTER APPLY l IN o.lines WHERE |", want: []string{"o"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var names []string
			for _, alias := range contextAt(t, tt.query).Aliases {
				names = append(names, alias.Name)
			}

			assert.Equal(t, tt.want, names)
		})
	}
}
