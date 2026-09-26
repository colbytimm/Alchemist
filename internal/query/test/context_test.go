package query_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

// at reads a query with the cursor marked by |, which is not a token of the
// language.
func at(marked string) (string, int) {
	cursor := strings.IndexByte(marked, '|')
	return marked[:cursor] + marked[cursor+1:], cursor
}

func contextAt(t *testing.T, marked string) query.Completion {
	t.Helper()
	text, cursor := at(marked)
	require.GreaterOrEqual(t, cursor, 0, "mark the cursor with |")
	return query.Context(text, cursor)
}

var (
	orders    = []string{"sales", "orders"}
	customers = []string{"sales", "customers"}
	archive   = []string{"sales", "archive"}
	products  = []string{"sales", "products"}
	mockJoin  = "SELECT o.id FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id"
)

func TestContextKeywords(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "empty buffer", query: "|", want: []string{"SELECT", "UPDATE", "DELETE"}},
		{name: "start of a word", query: "SEL|", want: []string{"SELECT", "UPDATE", "DELETE"}},
		{name: "after a source", query: "SELECT * FROM c |", want: []string{"AS", "WHERE", "JOIN", "INNER", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after an aliased source", query: "SELECT * FROM sales.orders o |", want: []string{"WHERE", "JOIN", "INNER", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after a container list", query: "SELECT * FROM sales.orders, sales.archive AS c |", want: []string{"WHERE", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after INNER", query: "SELECT * FROM c INNER |", want: []string{"JOIN"}},
		{name: "after a joined container", query: "SELECT * FROM sales.orders o JOIN sales.customers |", want: []string{"AS", "ON"}},
		{name: "after a joined container with an alias", query: "SELECT * FROM sales.orders o JOIN sales.customers cu |", want: []string{"ON"}},
		{name: "after the alias of a property join", query: "SELECT * FROM c JOIN t |", want: []string{"IN"}},
		{name: "after a property join", query: "SELECT * FROM c JOIN t IN c.tags |", want: []string{"WHERE", "JOIN", "INNER", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after a complete ON", query: mockJoin + " |", want: []string{"WHERE", "JOIN", "INNER"}},
		{name: "after INNER following a complete ON", query: mockJoin + " INNER |", want: []string{"JOIN"}},
		{name: "after INNER following a property join", query: "SELECT * FROM c JOIN t IN c.tags INNER |", want: []string{"JOIN"}},
		{name: "a keyword being typed after a starred select list", query: "SELECT * FR|", want: []string{"FROM", "AS"}},
		{name: "after a select item", query: "SELECT c.id |", want: []string{"FROM", "AS"}},
		{name: "after a value in WHERE", query: "SELECT * FROM c WHERE c.a = 1 |", want: []string{"AND", "OR", "NOT", "IN", "LIKE", "BETWEEN", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after a value in a join WHERE", query: mockJoin + " WHERE cu.region = 'west' |", want: []string{"AND", "OR", "NOT", "IN", "LIKE", "BETWEEN"}},
		{name: "after a LIKE pattern", query: "SELECT * FROM c WHERE c.a LIKE 'x%' |", want: []string{"ESCAPE", "AND", "OR", "NOT", "IN", "LIKE", "BETWEEN", "GROUP BY", "ORDER BY", "OFFSET"}},
		{name: "after NOT following a value", query: "SELECT * FROM c WHERE c.a NOT |", want: []string{"IN", "LIKE", "BETWEEN"}},
		{name: "after ORDER", query: "SELECT * FROM c ORDER |", want: []string{"BY"}},
		{name: "after an ORDER BY expression", query: "SELECT * FROM c ORDER BY c.ts |", want: []string{"ASC", "DESC", "OFFSET"}},
		{name: "after a sort direction", query: "SELECT * FROM c ORDER BY c.ts DESC |", want: []string{"OFFSET"}},
		{name: "after a GROUP BY expression", query: "SELECT * FROM c GROUP BY c.region |", want: []string{"ORDER BY", "OFFSET"}},
		{name: "after an offset", query: "SELECT * FROM c OFFSET 10 |", want: []string{"LIMIT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)

			assert.Equal(t, query.CompleteKeyword, got.Kind)
			assert.Equal(t, tt.want, got.Keywords)
		})
	}
}

func TestContextOffersNothing(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "inside a string", query: "SELECT * FROM c WHERE c.a = 'sel|ect'"},
		{name: "at the end of an unterminated string", query: "SELECT * FROM c WHERE c.a = 'open|"},
		{name: "inside a comment", query: "SELECT * FROM c -- WHE|\nWHERE c.a = 1"},
		{name: "at the end of a comment", query: "SELECT * FROM c -- the one|"},
		{name: "after TOP", query: "SELECT TOP |"},
		{name: "after AS", query: "SELECT * FROM sales.orders AS |"},
		{name: "after a join modifier the planner refuses", query: "SELECT * FROM sales.orders o LEFT |"},
		{name: "a property join alongside a cross-container join", query: mockJoin + " JOIN t |"},
		{name: "after a database and container in a source", query: "SELECT * FROM sales.orders.|"},
		{name: "after an offset keyword", query: "SELECT * FROM c OFFSET |"},
		{name: "after a limit", query: "SELECT * FROM c OFFSET 0 LIMIT 10 |"},
		{name: "an ON side waiting for its equals", query: "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId |"},
		{name: "an alias being typed after a source", query: "SELECT * FROM sales.orders o|"},
		{name: "an alias being typed after a joined source", query: "SELECT * FROM sales.orders o JOIN sales.customers cu|"},
		{name: "a name being typed after a select item", query: "SELECT c.id total|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, query.CompleteNothing, contextAt(t, tt.query).Kind)
		})
	}
}

func TestContextSources(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		want     query.CompletionKind
		database string
	}{
		{name: "after FROM", query: "SELECT * FROM |", want: query.CompleteSource},
		{name: "a database being typed", query: "SELECT * FROM sa|", want: query.CompleteSource},
		{name: "after a list comma", query: "SELECT * FROM sales.orders, sa|", want: query.CompleteDatabase},
		{name: "after JOIN", query: "SELECT * FROM sales.orders o JOIN sa|", want: query.CompleteDatabase},
		{name: "a container of a database", query: "SELECT * FROM sales.or|", want: query.CompleteContainer, database: "sales"},
		{name: "a container after JOIN", query: "SELECT * FROM sales.orders o JOIN sales.|", want: query.CompleteContainer, database: "sales"},
		{name: "a third join source", query: mockJoin + " JOIN |", want: query.CompleteDatabase},
		{name: "a container after a list comma", query: "SELECT * FROM sales.orders, sales.|", want: query.CompleteContainer, database: "sales"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)

			assert.Equal(t, tt.want, got.Kind)
			assert.Equal(t, tt.database, got.Database)
		})
	}
}

func TestContextFields(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		alias    query.Alias
		path     []string
		topLevel bool
	}{
		{
			name:  "the alias of a container",
			query: "SELECT * FROM sales.orders o WHERE o.cu|",
			alias: query.Alias{Name: "o", Scopes: [][]string{orders}},
		},
		{
			name:  "an AS alias",
			query: "SELECT * FROM sales.orders AS o WHERE o.|",
			alias: query.Alias{Name: "o", Scopes: [][]string{orders}},
		},
		{
			name:  "a nested path",
			query: "SELECT * FROM sales.orders o WHERE o.customer.|",
			alias: query.Alias{Name: "o", Scopes: [][]string{orders}},
			path:  []string{"customer"},
		},
		{
			name:  "the default alias of an unaliased container",
			query: "SELECT c.| FROM sales.orders",
			alias: query.Alias{Name: "c", Scopes: [][]string{orders}},
		},
		{
			name:  "a bare source is left for the catalog scope",
			query: "SELECT * FROM c WHERE c.|",
			alias: query.Alias{Name: "c"},
		},
		{
			name:  "an alias nothing declares is left for the catalog scope",
			query: "SELECT * FROM sales.orders o WHERE x.|",
			alias: query.Alias{Name: "x"},
		},
		{
			name:  "the alias of a property join",
			query: "SELECT t.| FROM sales.orders o JOIN t IN o.lines",
			alias: query.Alias{Name: "t", Scopes: [][]string{orders}, Path: []string{"lines[]"}},
		},
		{
			name:  "a property join under a nested path",
			query: "SELECT t.| FROM c JOIN t IN c.customer.tags",
			alias: query.Alias{Name: "t", Path: []string{"customer", "tags[]"}},
		},
		{
			name:  "the left side of a join",
			query: mockJoin + " WHERE o.|",
			alias: query.Alias{Name: "o", Scopes: [][]string{orders}},
		},
		{
			name:  "the right side of a join",
			query: mockJoin + " WHERE cu.|",
			alias: query.Alias{Name: "cu", Scopes: [][]string{customers}},
		},
		{
			name:  "a join side with no alias goes by its container name",
			query: "SELECT * FROM sales.orders JOIN sales.customers ON orders.customerId = customers.id WHERE customers.|",
			alias: query.Alias{Name: "customers", Scopes: [][]string{customers}},
		},
		{
			name:  "a union alias covers every listed container",
			query: "SELECT * FROM sales.orders, sales.archive AS c WHERE c.|",
			alias: query.Alias{Name: "c", Scopes: [][]string{orders, archive}},
		},
		{
			name:  "an ON side walks nested paths",
			query: "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customer.|",
			alias: query.Alias{Name: "o", Scopes: [][]string{orders}},
			path:  []string{"customer"},
		},
		{
			name:     "the SELECT list of a join is top-level only",
			query:    "SELECT o.| FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id",
			alias:    query.Alias{Name: "o", Scopes: [][]string{orders}},
			topLevel: true,
		},
		{
			name:  "text before the cursor may be multi-byte",
			query: "SELECT * FROM c WHERE c.name = 'Zoë' AND c.|",
			alias: query.Alias{Name: "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)

			require.Equal(t, query.CompleteField, got.Kind)
			assert.Equal(t, []query.Alias{tt.alias}, got.Aliases)
			assert.Equal(t, tt.path, got.Path)
			assert.Equal(t, tt.topLevel, got.TopLevel)
		})
	}
}

func TestContextReferencesAndExpressions(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		want     query.CompletionKind
		aliases  []query.Alias
		keywords []string
	}{
		{
			name:    "after ON",
			query:   "SELECT * FROM sales.orders o JOIN sales.customers cu ON |",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "o", Scopes: [][]string{orders}}, {Name: "cu", Scopes: [][]string{customers}}},
		},
		{
			name:    "after the equals of ON behind an earlier side",
			query:   "SELECT * FROM sales.orders o JOIN sales.customers cu ON o.customerId = |",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "cu", Scopes: [][]string{customers}}},
		},
		{
			name:    "after ON of a third container",
			query:   mockJoin + " JOIN sales.products p ON |",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "o", Scopes: [][]string{orders}}, {Name: "cu", Scopes: [][]string{customers}}, {Name: "p", Scopes: [][]string{products}}},
		},
		{
			name:    "after the equals of ON behind the joined side",
			query:   mockJoin + " JOIN sales.products p ON p.id = |",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "o", Scopes: [][]string{orders}}, {Name: "cu", Scopes: [][]string{customers}}},
		},
		{
			name:    "an ON never reads a container joined further right",
			query:   "SELECT * FROM sales.orders o JOIN sales.customers cu ON | JOIN sales.products p ON o.sku = p.id",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "o", Scopes: [][]string{orders}}, {Name: "cu", Scopes: [][]string{customers}}},
		},
		{
			name:    "after IN of a property join",
			query:   "SELECT * FROM sales.orders o JOIN t IN |",
			want:    query.CompleteReference,
			aliases: []query.Alias{{Name: "o", Scopes: [][]string{orders}}},
		},
		{
			name:     "the start of a WHERE clause",
			query:    "SELECT * FROM sales.orders o WHERE |",
			want:     query.CompleteExpression,
			aliases:  []query.Alias{{Name: "o", Scopes: [][]string{orders}}},
			keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:     "a function being typed",
			query:    "SELECT * FROM c WHERE STARTS|",
			want:     query.CompleteExpression,
			aliases:  []query.Alias{{Name: "c"}},
			keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:     "after an operator",
			query:    "SELECT * FROM c WHERE c.a = |",
			want:     query.CompleteExpression,
			aliases:  []query.Alias{{Name: "c"}},
			keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:     "inside parentheses a subquery may open",
			query:    "SELECT * FROM c WHERE c.a IN (|",
			want:     query.CompleteExpression,
			aliases:  []query.Alias{{Name: "c"}},
			keywords: []string{"SELECT", "NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:     "the start of a SELECT list",
			query:    "SELECT |",
			want:     query.CompleteExpression,
			keywords: []string{"DISTINCT", "TOP", "VALUE", "NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:     "after TOP and its count",
			query:    "SELECT TOP 5 |",
			want:     query.CompleteExpression,
			keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
		{
			name:    "the start of an ORDER BY",
			query:   "SELECT * FROM c ORDER BY |",
			want:    query.CompleteExpression,
			aliases: []query.Alias{{Name: "c"}},
		},
		{
			name:  "a property join alias is in scope for expressions",
			query: "SELECT * FROM sales.orders o JOIN t IN o.lines WHERE |",
			want:  query.CompleteExpression,
			aliases: []query.Alias{
				{Name: "o", Scopes: [][]string{orders}},
				{Name: "t", Scopes: [][]string{orders}, Path: []string{"lines[]"}},
			},
			keywords: []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.query)

			require.Equal(t, tt.want, got.Kind)
			assert.Equal(t, tt.aliases, got.Aliases)
			assert.Equal(t, tt.keywords, got.Keywords)
		})
	}
}

func TestContextWordAndRange(t *testing.T) {
	tests := []struct {
		name  string
		query string
		word  string
		token string
	}{
		{name: "at offset zero", query: "|SELECT", word: "", token: "SELECT"},
		{name: "at the end of a word", query: "SEL|", word: "SEL", token: "SEL"},
		{name: "in the middle of a word covers the whole word", query: "SELECT * FROM c WHERE c.cu|stomer", word: "cu", token: "customer"},
		{name: "after whitespace", query: "SELECT * FROM c |", word: "", token: ""},
		{name: "after a dot", query: "SELECT c.| FROM c", word: "", token: ""},
		{name: "at the end", query: "SELECT * FROM c WHERE c.a|", word: "a", token: "a"},
		{name: "after multi-byte text", query: "SELECT * FROM c WHERE c.a = 'Zoë' AND c.na|", word: "na", token: "na"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, cursor := at(tt.query)

			got := query.Context(text, cursor)

			assert.Equal(t, tt.word, got.Word)
			assert.Equal(t, tt.token, text[got.Start:got.End])
			if tt.token == "" {
				assert.Equal(t, cursor, got.Start)
			}
		})
	}
}

func TestWithDefaultScopeBindsOnlyUnboundAliases(t *testing.T) {
	c := query.Completion{Aliases: []query.Alias{{Name: "c"}, {Name: "o", Scopes: [][]string{orders}}}}

	bound := c.WithDefaultScope(customers)

	assert.Equal(t, [][]string{customers}, bound.Aliases[0].Scopes)
	assert.Equal(t, [][]string{orders}, bound.Aliases[1].Scopes)
	assert.Empty(t, c.Aliases[0].Scopes, "the original is untouched")
	assert.Equal(t, c, c.WithDefaultScope(nil))
}

func TestAnApostropheInACommentDoesNotOpenAString(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM sales.orders o -- the customer's orders\nWHERE o.total > 1")
	require.NoError(t, err)

	assert.Equal(t, []string{"sales", "orders"}, plan.Scope())
	assert.Equal(t, "SELECT * FROM o -- the customer's orders\nWHERE o.total > 1", plan.Leaves[0].Query.Text)
}

func FuzzContextRangeStaysInsideTheText(f *testing.F) {
	for _, seed := range []string{
		"SELECT * FROM c", "SELECT c.| FROM c WHERE c.a = 'é'", "-- x\nSELECT", `'\`, "FROM sales.", "ORDER BY c.ts DESC",
		"DELETE FROM sales.orders o WHERE o.",
	} {
		for cursor := -1; cursor <= len(seed)+1; cursor++ {
			f.Add(seed, cursor)
		}
	}

	f.Fuzz(func(t *testing.T, text string, cursor int) {
		got := query.Context(text, cursor)

		require.GreaterOrEqual(t, got.Start, 0)
		require.LessOrEqual(t, got.Start, got.End)
		require.LessOrEqual(t, got.End, len(text))
		require.True(t, strings.HasPrefix(text[got.Start:got.End], got.Word))
		if utf8.ValidString(text) {
			require.True(t, got.Start == len(text) || utf8.RuneStart(text[got.Start]))
			require.True(t, got.End == len(text) || utf8.RuneStart(text[got.End]))
		}
	})
}

func TestContextInABatch(t *testing.T) {
	const header = `BEGIN BATCH sales.orders PARTITION "c01";`
	tests := []struct {
		name  string
		batch string
		want  query.Completion
	}{
		{name: "after BEGIN", batch: "BEGIN |BATCH", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"BATCH"}}},
		{name: "the target's database", batch: "BEGIN BATCH |", want: query.Completion{Kind: query.CompleteDatabase}},
		{name: "the target's container", batch: "BEGIN BATCH sales.|", want: query.Completion{Kind: query.CompleteContainer, Database: "sales"}},
		{name: "after the target", batch: "BEGIN BATCH sales.orders |", want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"PARTITION"}}},
		{name: "the start of an operation", batch: header + "\n  |", want: query.Completion{Kind: query.CompleteKeyword,
			Keywords: []string{"CREATE", "UPSERT", "REPLACE", "DELETE", "READ", "PATCH", "COMMIT"}}},
		{name: "after IF", batch: header + ` DELETE "a" IF |`, want: query.Completion{Kind: query.CompleteKeyword, Keywords: []string{"MATCH"}}},
		{name: "inside a body", batch: header + ` CREATE {"id": "a", |`},
		{name: "inside a patch array", batch: header + ` PATCH "a" [{"op": "set"}, |`},
		{name: "after an id", batch: header + ` READ "a" |`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextAt(t, tt.batch)

			assert.Equal(t, tt.want.Kind, got.Kind)
			assert.Equal(t, tt.want.Keywords, got.Keywords)
			assert.Equal(t, tt.want.Database, got.Database)
		})
	}
}
