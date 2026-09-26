package query_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

func TestSourcePaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [][]string
	}{
		{name: "one source", input: "SELECT * FROM sales.orders o", want: [][]string{{"sales", "orders"}}},
		{
			name:  "container list",
			input: "SELECT * FROM sales.orders, sales.archive",
			want:  [][]string{{"sales", "orders"}, {"sales", "archive"}},
		},
		{
			name:  "cross-container join",
			input: "SELECT * FROM sales.orders o JOIN sales.customers c ON o.customerId = c.id",
			want:  [][]string{{"sales", "orders"}, {"sales", "customers"}},
		},
		{
			name: "multi-way join",
			input: "SELECT * FROM sales.orders o JOIN sales.customers c ON o.customerId = c.id " +
				"JOIN sales.products p ON o.sku = p.id",
			want: [][]string{{"sales", "orders"}, {"sales", "customers"}, {"sales", "products"}},
		},
		{
			name:  "join over a property is not a source",
			input: "SELECT t.name FROM c JOIN t IN c.tags",
			want:  [][]string{{"c"}},
		},
		{name: "three-part path", input: "SELECT * FROM staging.sales.customers c", want: [][]string{{"staging", "sales", "customers"}}},
		{
			name:  "source inside a subquery",
			input: "SELECT * FROM (SELECT * FROM sub.things) outer",
			want:  [][]string{{"sub", "things"}},
		},
		{name: "path inside a string literal", input: `SELECT * FROM c WHERE c.note = "FROM a.b.c"`, want: [][]string{{"c"}}},
		{name: "no source", input: "SELECT 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, query.SourcePaths(tt.input), "SourcePaths(%q)", tt.input)
		})
	}
}

func FuzzSourcePathsNeverPanics(f *testing.F) {
	for _, seed := range plannerSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		for _, path := range query.SourcePaths(input) {
			require.NotEmpty(t, path)
		}
	})
}
