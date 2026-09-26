package query_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

const goalDelete = `DELETE FROM sales.orders o WHERE o.status = "cancelled"`

func TestParseMutationReadsTheGoalDelete(t *testing.T) {
	m := parseMutation(t, goalDelete)

	assert.Equal(t, query.Mutation{
		Kind:   query.MutationDelete,
		Target: []string{"sales", "orders"},
		Alias:  "o",
		Where:  `o.status = "cancelled"`,
	}, m)
}

func TestParseDeleteShapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		alias string
		where string
	}{
		{name: "the default alias", input: "DELETE FROM a.b WHERE c.y = 2", alias: "c", where: "c.y = 2"},
		{name: "a bare alias", input: "DELETE FROM a.b o WHERE o.y = 2", alias: "o", where: "o.y = 2"},
		{name: "AS", input: "delete from a.b as item where item.y = 2", alias: "item", where: "item.y = 2"},
		{name: "keywords in any case", input: "Delete From a.b o Where o.y = 2", alias: "o", where: "o.y = 2"},
		{name: "a trailing semicolon", input: "DELETE FROM a.b o WHERE o.y = 2;", alias: "o", where: "o.y = 2"},
		{name: "a comment first", input: "-- tidy up\nDELETE FROM a.b o WHERE o.y = 2", alias: "o", where: "o.y = 2"},
		{
			name: "a subquery over the item's own array", input: `DELETE FROM a.b o WHERE EXISTS(SELECT VALUE t FROM t IN o.tags WHERE t = "x")`,
			alias: "o", where: `EXISTS(SELECT VALUE t FROM t IN o.tags WHERE t = "x")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := parseMutation(t, tt.input)

			assert.Equal(t, query.MutationDelete, m.Kind)
			assert.Equal(t, []string{"a", "b"}, m.Target)
			assert.Equal(t, tt.alias, m.Alias)
			assert.Equal(t, tt.where, m.Where)
			assert.Empty(t, m.Assignments)
			assert.Empty(t, m.Removals)
		})
	}
}

func TestADeleteSendsItsConditionWithoutComments(t *testing.T) {
	m := parseMutation(t, "DELETE FROM a.b o WHERE o.a = 1 -- note\n AND o.b = 2")

	assert.NotContains(t, m.Where, "--")
	assert.NotContains(t, m.Where, "note")
	assert.Contains(t, m.Where, "AND o.b = 2")
}

func TestEveryItemIsTrueHoweverParenthesized(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "DELETE FROM a.b o WHERE true", want: true},
		{input: "DELETE FROM a.b o WHERE (true)", want: true},
		{input: "DELETE FROM a.b o WHERE (( TRUE ))", want: true},
		{input: "DELETE FROM a.b o WHERE true AND o.y = 1"},
		{input: "DELETE FROM a.b o WHERE (true) AND (o.y = 1)"},
		{input: "UPDATE a.b o SET o.x = 1 WHERE (true)", want: true},
		{input: "UPDATE a.b o SET o.x = 1 WHERE ((true));", want: true},
		{input: "UPDATE a.b o SET o.x = 1 WHERE (true) OR (false)"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, parseMutation(t, tt.input).EveryItem)
		})
	}
}

func TestParseDeleteRefusals(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		line        int
		column      int
		message     string
		unsupported bool
	}{
		{name: "no WHERE", input: "DELETE FROM a.b o", line: 1, column: 18, message: "DELETE needs a WHERE. To delete every item, write WHERE true"},
		{name: "no WHERE before the semicolon", input: "DELETE FROM a.b o;", line: 1, column: 18, message: "DELETE needs a WHERE"},
		{name: "an empty WHERE", input: "DELETE FROM a.b o WHERE", line: 1, column: 24, message: "WHERE needs a condition"},
		{
			name: "no FROM", input: `DELETE sales.orders o WHERE o.status = "x"`, line: 1, column: 8,
			message: "DELETE needs FROM: DELETE FROM sales.orders o WHERE …",
		},
		{name: "DELETE alone", input: "DELETE", line: 1, column: 7, message: "DELETE needs FROM: DELETE FROM database.container …"},
		{name: "an alias before FROM", input: "DELETE o FROM sales.orders o WHERE true", line: 1, column: 8, message: "a DELETE has one target container", unsupported: true},
		{name: "a field before FROM", input: "DELETE o.tmp FROM sales.orders o WHERE true", line: 1, column: 8, message: "DELETE removes whole items: removing a field is UPDATE … UNSET"},
		{name: "USING", input: "DELETE FROM a.b o USING x.y z WHERE true", line: 1, column: 19, message: "a DELETE has one target container", unsupported: true},
		{name: "USING without an alias", input: "DELETE FROM a.b USING x.y z WHERE true", line: 1, column: 17, message: "a DELETE has one target container", unsupported: true},
		{name: "JOIN", input: "DELETE FROM a.b o JOIN x.y z ON o.k = z.k WHERE true", line: 1, column: 19, message: "a DELETE has one target container", unsupported: true},
		{name: "two targets", input: "DELETE FROM a.b, a.c WHERE true", line: 1, column: 16, message: "a DELETE has one target container", unsupported: true},
		{name: "WITH", input: "WITH x AS (SELECT * FROM a.b) DELETE FROM a.b o WHERE true", line: 1, column: 1, message: "a DELETE has one target container", unsupported: true},
		{name: "a container alone", input: "DELETE FROM orders o WHERE true", line: 1, column: 13, message: "name the target as database.container"},
		{
			name: "another container in the WHERE", input: "DELETE FROM a.b o\nWHERE NOT EXISTS(SELECT VALUE 1 FROM a.customers k WHERE k.id = o.customerId)",
			line: 2, column: 38, message: "Run the join as a query and delete WHERE o.id IN (…)", unsupported: true,
		},
		{name: "TOP", input: "DELETE TOP 10 FROM a.b o WHERE true", line: 1, column: 8, message: "TOP in a DELETE", unsupported: true},
		{name: "ORDER BY", input: "DELETE FROM a.b o WHERE true ORDER BY o.x", line: 1, column: 30, message: "ORDER in a DELETE", unsupported: true},
		{name: "OFFSET", input: "DELETE FROM a.b o WHERE true OFFSET 1", line: 1, column: 30, message: "OFFSET in a DELETE", unsupported: true},
		{name: "LIMIT", input: "DELETE FROM a.b o WHERE true LIMIT 1", line: 1, column: 30, message: "LIMIT in a DELETE", unsupported: true},
		{name: "RETURNING", input: "DELETE FROM a.b o WHERE true RETURNING o", line: 1, column: 30, message: "RETURNING in a DELETE", unsupported: true},
		{name: "text after the semicolon", input: "DELETE FROM a.b o WHERE true;\nSELECT * FROM c", line: 2, column: 1, message: "a buffer holds one statement"},
		{
			name: "a WHERE that closes its own parenthesis", input: `DELETE FROM sales.orders o WHERE o.status = "cancelled") OR (true`,
			line: 1, column: 56, message: "unbalanced parentheses in the WHERE",
		},
		{
			name: "ORDER BY after a closing parenthesis", input: "DELETE FROM a.b o WHERE o.x = 1) ORDER BY o.x",
			line: 1, column: 34, message: "ORDER in a DELETE", unsupported: true,
		},
		{
			name: "a ) after a comment the service ends at \\r", input: "DELETE FROM a.b o WHERE o.a = 1 -- x\r) OR (true\n AND o.b = 2",
			line: 1, column: 38, message: "unbalanced parentheses in the WHERE",
		},
		{name: "an unclosed parenthesis", input: "DELETE FROM a.b o WHERE (o.x = 1", line: 1, column: 32, message: "unbalanced parentheses in the WHERE"},
		{name: "a comma after an aliased target", input: "DELETE FROM a.b o, a.c WHERE true", line: 1, column: 18, message: "a DELETE has one target container", unsupported: true},
		{name: "an update's comma after an aliased target", input: "UPDATE a.b o, a.c SET o.x = 1 WHERE true", line: 1, column: 13, message: "an UPDATE has one target container", unsupported: true},
		{name: "a keyword after AS", input: "DELETE FROM s.o AS WHERE WHERE true", line: 1, column: 20, message: "expected an alias after AS"},
		{name: "an update's keyword after AS", input: "UPDATE s.o AS SET SET s.x = 1 WHERE true", line: 1, column: 15, message: "expected an alias after AS"},
		{name: "SET", input: "DELETE FROM a.b o SET o.x = 1 WHERE true", line: 1, column: 19, message: "expected WHERE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.ParseMutation(tt.input)

			var syntax *query.MutationSyntaxError
			require.ErrorAs(t, err, &syntax)
			assert.Contains(t, syntax.Message, tt.message)
			assert.Equal(t, tt.line, syntax.Line, "line")
			assert.Equal(t, tt.column, syntax.Column, "column")
			assert.Equal(t, tt.unsupported, errors.Is(err, query.ErrMutationUnsupported))
		})
	}
}

func TestIsMutationSeesADelete(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "the goal statement", input: goalDelete, want: true},
		{name: "lower case after a comment", input: "-- note\ndelete from a.b", want: true},
		{name: "DELETE without FROM, to be refused", input: "DELETE a.b o WHERE true", want: true},
		{name: "WITH, to be refused", input: "WITH x AS (SELECT 1) DELETE FROM a.b", want: true},
		{name: "DELETE in a string", input: `SELECT * FROM c WHERE c.note = "DELETE FROM a.b"`},
		{name: "DELETE in a comment", input: "-- DELETE FROM a.b\nSELECT * FROM c"},
		{name: "a field called delete", input: "SELECT c.delete FROM c"},
		{name: "TRUNCATE", input: "TRUNCATE a.b"},
		{name: "a field called delete after a CTE", input: "WITH x AS (SELECT c.id FROM c) SELECT c.delete FROM c"},
		{name: "a field called delete inside a CTE", input: "WITH x AS (SELECT c.delete FROM c) SELECT * FROM x"},
		{name: "a CTE called delete", input: "WITH delete AS (SELECT * FROM a.b) SELECT * FROM delete"},
		{name: "a CTE called update", input: "WITH update AS (SELECT * FROM a.b) SELECT * FROM update"},
		{name: "two CTEs then a delete", input: "WITH x AS (SELECT 1), y AS (SELECT (2)) DELETE FROM a.b o WHERE true", want: true},
		{name: "a CTE then a delete", input: "WITH x AS (SELECT c.id FROM c) DELETE FROM a.b o WHERE true", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, query.IsMutation(tt.input))
		})
	}
}

func TestMutationKindOf(t *testing.T) {
	kind, ok := query.MutationKindOf(goalDelete)
	assert.True(t, ok)
	assert.Equal(t, query.MutationDelete, kind)
	kind, ok = query.MutationKindOf(goalUpdate)
	assert.True(t, ok)
	assert.Equal(t, query.MutationUpdate, kind)
	_, ok = query.MutationKindOf("SELECT * FROM c")
	assert.False(t, ok)
}

func TestMutationTargetOfADelete(t *testing.T) {
	assert.Equal(t, []string{"prod", "sales", "orders"}, query.MutationTarget("DELETE FROM prod.sales.orders o WHERE true"))
	assert.Equal(t, []string{"sales", "orders"}, query.MutationTarget(goalDelete))
	assert.Nil(t, query.MutationTarget("DELETE sales.orders o WHERE true"))
}

func TestADeleteFormatsBackToItself(t *testing.T) {
	m := parseMutation(t, "delete from sales.orders where c.x = 1;")

	assert.Equal(t, "DELETE FROM sales.orders AS c WHERE c.x = 1", m.String())
	assert.Equal(t, m, parseMutation(t, m.String()))
}

func TestTheVerbsOfEachKind(t *testing.T) {
	tests := []struct {
		kind    query.MutationKind
		name    string
		applied string
		ongoing string
	}{
		{kind: query.MutationUpdate, name: "update", applied: "updated", ongoing: "updating"},
		{kind: query.MutationDelete, name: "delete", applied: "deleted", ongoing: "deleting"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.name, tt.kind.String())
			assert.Equal(t, tt.applied, tt.kind.Applied())
			assert.Equal(t, tt.ongoing, tt.kind.Ongoing())
		})
	}
}

func TestCheckMutationOfADelete(t *testing.T) {
	tests := []struct {
		where string
		warn  string
	}{
		{where: `o.customerId = "c01" AND o.status = "cancelled"`},
		{where: `o.status = "cancelled"`, warn: "The WHERE does not pin /customerId: the selection reads every partition."},
		{
			where: "true",
			warn: "Every item in sales.orders. Deleting and recreating the container (d, then c in the catalog) " +
				"spends no RU per item, but it is a new container and its settings must be given again.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			check := query.CheckMutation(parseMutation(t, "DELETE FROM sales.orders o WHERE "+tt.where), customerKey)

			assert.Empty(t, check.Problems)
			if tt.warn == "" {
				assert.Empty(t, check.Warnings)
				return
			}
			assert.Equal(t, []string{tt.warn}, check.Warnings)
		})
	}
}

func TestDeleteContext(t *testing.T) {
	target := query.Alias{Name: "o", Scopes: [][]string{orders}}
	tests := []struct {
		name  string
		query string
		want  query.Completion
	}{
		{name: "only FROM after DELETE", query: "DELETE |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"FROM"}}},
		{name: "nothing without FROM", query: "DELETE sales.|"},
		{name: "the target's database", query: "DELETE FROM |", want: query.Completion{Kind: query.CompleteDatabase}},
		{name: "the target's container", query: "DELETE FROM sales.|", want: query.Completion{Kind: query.CompleteContainer, Database: "sales"}},
		{name: "after the target", query: "DELETE FROM sales.orders |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"WHERE", "AS"}}},
		{name: "an alias being invented", query: "DELETE FROM sales.orders ord|"},
		{name: "after the alias", query: "DELETE FROM sales.orders o |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"WHERE"}}},
		{
			name: "the condition", query: "DELETE FROM sales.orders o WHERE |",
			want: query.Completion{Kind: query.CompleteExpression, Aliases: []query.Alias{target},
				Keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"}},
		},
		{
			name: "any field in the condition", query: "DELETE FROM sales.orders o WHERE o.|",
			want: query.Completion{Kind: query.CompleteField, Aliases: []query.Alias{target}},
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

func TestDeleteIsAKeywordOnlyInADelete(t *testing.T) {
	require.Equal(t, []spanText{
		{query.SpanKeyword, "DELETE"}, {query.SpanKeyword, "FROM"}, {query.SpanKeyword, "WHERE"},
		{query.SpanString, `"cancelled"`},
	}, spanTexts(goalDelete))
	require.Equal(t, []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanKeyword, "FROM"}},
		spanTexts("SELECT delete FROM c"), "DELETE is a name outside a delete")
}
