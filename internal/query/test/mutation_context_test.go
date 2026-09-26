package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/query"
)

func TestUpdateContext(t *testing.T) {
	target := query.Alias{Name: "o", Scopes: [][]string{orders}}
	defaultAlias := query.Alias{Name: "c", Scopes: [][]string{orders}}
	tests := []struct {
		name  string
		query string
		want  query.Completion
	}{
		{name: "the target's database", query: "UPDATE |", want: query.Completion{Kind: query.CompleteDatabase}},
		{name: "the target's container", query: "UPDATE sales.|", want: query.Completion{Kind: query.CompleteContainer, Database: "sales"}},
		{name: "after the target", query: "UPDATE sales.orders |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"SET", "UNSET", "AS"}}},
		{name: "an alias being invented", query: "UPDATE sales.orders ord|"},
		{name: "after AS", query: "UPDATE sales.orders AS |"},
		{name: "after the alias", query: "UPDATE sales.orders o |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"SET", "UNSET"}}},
		{
			name: "a field to set", query: "UPDATE sales.orders o SET |",
			want: query.Completion{Kind: query.CompleteReference, Aliases: []query.Alias{target}, Writable: true},
		},
		{
			name: "a field under the alias", query: "UPDATE sales.orders o SET o.|",
			want: query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{target}, Writable: true},
		},
		{
			name: "a nested field under the default alias", query: "UPDATE sales.orders SET c.shipTo.|",
			want: query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{defaultAlias}, Path: []string{"shipTo"}, Writable: true},
		},
		{name: "another alias", query: "UPDATE sales.orders o SET x.|"},
		{name: "a value", query: "UPDATE sales.orders o SET o.x = |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"TRUE", "FALSE", "NULL"}}},
		{name: "no field after =", query: "UPDATE sales.orders o SET o.x = o.|"},
		{name: "after a value", query: "UPDATE sales.orders o SET o.x = 1 |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"UNSET", "WHERE"}}},
		{
			name: "the next assignment", query: "UPDATE sales.orders o SET o.x = 1, |",
			want: query.Completion{Kind: query.CompleteReference, Aliases: []query.Alias{target}, Writable: true},
		},
		{
			name: "a field to unset", query: "UPDATE sales.orders o SET o.x = 1 UNSET |",
			want: query.Completion{Kind: query.CompleteReference, Aliases: []query.Alias{target}, Writable: true},
		},
		{name: "after a path to unset", query: "UPDATE sales.orders o UNSET o.tmp |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"WHERE"}}},
		{
			name: "the condition", query: "UPDATE sales.orders o SET o.x = 1 WHERE |",
			want: query.Completion{Kind: query.CompleteExpression, Aliases: []query.Alias{target},
				Keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"}},
		},
		{
			name: "any field in the condition", query: "UPDATE sales.orders o SET o.x = 1 WHERE o.|",
			want: query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{target}},
		},
		{
			name: "after a value in the condition", query: "UPDATE sales.orders o SET o.x = 1 WHERE o.y = 2 |",
			want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"AND", "OR", "NOT", "IN", "LIKE", "BETWEEN"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)
			got.Word, got.Start, got.End = "", 0, 0

			assert.Equal(t, tt.want, got)
		})
	}
}
