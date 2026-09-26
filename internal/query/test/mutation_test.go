package query_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

const goalUpdate = `UPDATE sales.orders o
SET o.status = "archived", o.archivedAt = "2026-01-01"
WHERE o.status = "shipped" AND o.total < 50`

func parseMutation(t *testing.T, text string) query.Mutation {
	t.Helper()
	m, err := query.ParseMutation(text)
	require.NoError(t, err, text)
	return m
}

func TestParseMutationReadsTheGoalStatement(t *testing.T) {
	m := parseMutation(t, goalUpdate)

	assert.Equal(t, query.MutationUpdate, m.Kind)
	assert.Equal(t, []string{"sales", "orders"}, m.Target)
	assert.Equal(t, "o", m.Alias)
	require.Len(t, m.Assignments, 2)
	assert.Equal(t, "/status", m.Assignments[0].Path.Pointer())
	assert.JSONEq(t, `"archived"`, string(m.Assignments[0].Value))
	assert.Equal(t, "/archivedAt", m.Assignments[1].Path.Pointer())
	assert.Empty(t, m.Removals)
	assert.Equal(t, `o.status = "shipped" AND o.total < 50`, m.Where)
	assert.False(t, m.EveryItem)
}

func TestAPathBecomesAPointer(t *testing.T) {
	tests := []struct {
		path    string
		pointer string
	}{
		{path: "o.status", pointer: "/status"},
		{path: "o.shipTo.region", pointer: "/shipTo/region"},
		{path: `o["order-id"]`, pointer: "/order-id"},
		{path: `o['order-id']`, pointer: "/order-id"},
		{path: "o.lines[0].qty", pointer: "/lines/0/qty"},
		{path: `o["a/b"]["c~d"]`, pointer: "/a~1b/c~0d"},
		{path: "o.value", pointer: "/value"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			m := parseMutation(t, "UPDATE a.b o SET "+tt.path+" = 1 WHERE true")

			assert.Equal(t, tt.pointer, m.Assignments[0].Path.Pointer())
		})
	}
}

func TestAValueIsSentAsJSON(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: `"archived"`, want: `"archived"`},
		{value: `'say "hi"'`, want: `"say \"hi\""`},
		{value: `-12.5e3`, want: `-12.5e3`},
		{value: `12345678901234567890`, want: `12345678901234567890`},
		{value: `true`, want: `true`},
		{value: `FALSE`, want: `false`},
		{value: `null`, want: `null`},
		{value: `{"a": [1, {"b": null}]}`, want: `{"a":[1,{"b":null}]}`},
		{value: `[1, "two"]`, want: `[1,"two"]`},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			m := parseMutation(t, "UPDATE a.b o SET o.x = "+tt.value+" WHERE true")

			assert.Equal(t, tt.want, string(m.Assignments[0].Value))
		})
	}
}

func TestParseMutationShapes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		alias    string
		sets     int
		removals int
		where    string
	}{
		{name: "the default alias", input: "UPDATE a.b SET c.x = 1 WHERE c.y = 2", alias: "c", sets: 1, where: "c.y = 2"},
		{name: "AS", input: "update a.b as item set item.x = 1 where item.y = 2", alias: "item", sets: 1, where: "item.y = 2"},
		{name: "SET then UNSET", input: "UPDATE a.b o SET o.x = 1 UNSET o.y, o.z WHERE true", alias: "o", sets: 1, removals: 2, where: "true"},
		{name: "UNSET alone", input: "UPDATE a.b o UNSET o.tmp WHERE IS_DEFINED(o.tmp)", alias: "o", removals: 1, where: "IS_DEFINED(o.tmp)"},
		{name: "a trailing semicolon", input: "UPDATE a.b o SET o.x = 1 WHERE o.y = 2;", alias: "o", sets: 1, where: "o.y = 2"},
		{
			name: "a condition holding ;, SET and -- inside strings", input: `UPDATE a.b o SET o.x = 1 WHERE o.note = "a; SET o.y = 2 -- no" ;`,
			alias: "o", sets: 1, where: `o.note = "a; SET o.y = 2 -- no"`,
		},
		{
			name: "a subquery over the item's own array", input: `UPDATE a.b o SET o.x = 1 WHERE EXISTS(SELECT VALUE t FROM t IN o.tags WHERE t = "x")`,
			alias: "o", sets: 1, where: `EXISTS(SELECT VALUE t FROM t IN o.tags WHERE t = "x")`,
		},
		{
			name: "a comment after the statement", input: "UPDATE a.b o SET o.x = 1 WHERE o.y = 2 -- done",
			alias: "o", sets: 1, where: "o.y = 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := parseMutation(t, tt.input)

			assert.Equal(t, tt.alias, m.Alias)
			assert.Len(t, m.Assignments, tt.sets)
			assert.Len(t, m.Removals, tt.removals)
			assert.Equal(t, tt.where, m.Where)
		})
	}
}

func TestWhereTrueIsEveryItem(t *testing.T) {
	assert.True(t, parseMutation(t, "UPDATE a.b o SET o.x = 1 WHERE true").EveryItem)
	assert.True(t, parseMutation(t, "UPDATE a.b o SET o.x = 1 WHERE TRUE;").EveryItem)
	assert.False(t, parseMutation(t, "UPDATE a.b o SET o.x = 1 WHERE true AND o.y = 1").EveryItem)
}

func TestParseMutationRefusals(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		line        int
		column      int
		message     string
		unsupported bool
	}{
		{name: "no WHERE", input: "UPDATE a.b o\nSET o.x = 1", line: 2, column: 12, message: "UPDATE needs a WHERE. To change every item, write WHERE true"},
		{name: "no WHERE before the semicolon", input: "UPDATE a.b o SET o.x = 1;", line: 1, column: 25, message: "UPDATE needs a WHERE"},
		{name: "an empty WHERE", input: "UPDATE a.b o SET o.x = 1 WHERE", line: 1, column: 31, message: "WHERE needs a condition"},
		{name: "an expression", input: "UPDATE a.b o SET o.total = o.total * 1.1 WHERE true", line: 1, column: 28, message: "SET takes a literal JSON value", unsupported: true},
		{name: "another field", input: "UPDATE a.b o SET o.total = o.other WHERE true", line: 1, column: 28, message: "a patch cannot read another field", unsupported: true},
		{name: "a function", input: "UPDATE a.b o SET o.x = UPPER(o.x) WHERE true", line: 1, column: 24, message: "SET takes a literal JSON value", unsupported: true},
		{name: "a parameter", input: "UPDATE a.b o SET o.x = @p WHERE true", line: 1, column: 24, message: "SET takes a literal JSON value", unsupported: true},
		{name: "arithmetic on a literal", input: "UPDATE a.b o SET o.x = 5 * 2 WHERE true", line: 1, column: 24, message: "SET takes a literal JSON value", unsupported: true},
		{name: "+=", input: "UPDATE a.b o SET o.n += 1 WHERE true", line: 1, column: 22, message: "increment is not built", unsupported: true},
		{name: "INCREMENT", input: "UPDATE a.b o INCREMENT o.n WHERE true", line: 1, column: 14, message: "increment is not built", unsupported: true},
		{name: "the whole item", input: "UPDATE a.b o SET o = {\"id\":\"x\"} WHERE true", line: 1, column: 18, message: "SET needs a field: replacing whole items is a REPLACE in a BEGIN BATCH"},
		{name: "an append", input: "UPDATE a.b o SET o.tags[-] = 1 WHERE true", line: 1, column: 25, message: "appending to and moving within arrays is not built", unsupported: true},
		{name: "a negative index", input: "UPDATE a.b o SET o.tags[-1] = 1 WHERE true", line: 1, column: 25, message: "appending to and moving within arrays", unsupported: true},
		{name: "another alias", input: "UPDATE a.b o SET o2.status = 1 WHERE true", line: 1, column: 18, message: "o2.status: paths start with the target's alias o"},
		{name: "a container alone", input: "UPDATE orders o SET o.x = 1 WHERE true", line: 1, column: 8, message: "name the target as database.container: a write never depends on the catalog cursor"},
		{name: "FROM c style", input: "UPDATE FROM c SET c.x = 1 WHERE true", line: 1, column: 8, message: "name the target as database.container"},
		{name: "three parts", input: "UPDATE prod.sales.orders o SET o.x = 1 WHERE true", line: 1, column: 8, message: "name the target as database.container"},
		{name: "two targets", input: "UPDATE a.b, a.c SET c.x = 1 WHERE true", line: 1, column: 11, message: "an UPDATE has one target container", unsupported: true},
		{name: "FROM", input: "UPDATE a.b o FROM x.y z SET o.x = 1 WHERE true", line: 1, column: 14, message: "an UPDATE has one target container", unsupported: true},
		{name: "JOIN", input: "UPDATE a.b o JOIN x.y z ON o.k = z.k SET o.x = 1 WHERE true", line: 1, column: 14, message: "an UPDATE has one target container", unsupported: true},
		{name: "FROM after SET", input: "UPDATE a.b o SET o.x = 1 FROM x.y z WHERE true", line: 1, column: 26, message: "an UPDATE has one target container", unsupported: true},
		{name: "WITH", input: "WITH x AS (SELECT * FROM a.b) UPDATE a.b o SET o.x = 1 WHERE true", line: 1, column: 1, message: "an UPDATE has one target container", unsupported: true},
		{
			name: "another container in the WHERE", input: "UPDATE a.b o SET o.x = 1\nWHERE EXISTS(SELECT VALUE 1 FROM a.returns r WHERE r.orderId = o.id)",
			line: 2, column: 34, message: "the WHERE reads another container", unsupported: true,
		},
		{name: "TOP", input: "UPDATE TOP 10 a.b o SET o.x = 1 WHERE true", line: 1, column: 8, message: "TOP in an UPDATE", unsupported: true},
		{name: "ORDER BY", input: "UPDATE a.b o SET o.x = 1 WHERE true ORDER BY o.x", line: 1, column: 37, message: "ORDER in an UPDATE", unsupported: true},
		{name: "OFFSET", input: "UPDATE a.b o SET o.x = 1 WHERE true OFFSET 1", line: 1, column: 37, message: "OFFSET in an UPDATE", unsupported: true},
		{name: "LIMIT", input: "UPDATE a.b o SET o.x = 1 WHERE true LIMIT 1", line: 1, column: 37, message: "LIMIT in an UPDATE", unsupported: true},
		{name: "RETURNING", input: "UPDATE a.b o SET o.x = 1 WHERE true RETURNING o", line: 1, column: 37, message: "RETURNING in an UPDATE", unsupported: true},
		{name: "text after the semicolon", input: "UPDATE a.b o SET o.x = 1 WHERE true;\nSELECT * FROM c", line: 2, column: 1, message: "a buffer holds one statement"},
		{name: "no SET", input: "UPDATE a.b o WHERE true", line: 1, column: 14, message: "expected SET or UNSET"},
		{name: "no =", input: "UPDATE a.b o SET o.x 1 WHERE true", line: 1, column: 22, message: "expected = after o.x"},
		{name: "an unclosed string in the WHERE", input: `UPDATE a.b o SET o.x = 1 WHERE o.y = "open`, line: 1, column: 38, message: "never closed"},
		{name: "a number JSON cannot read", input: "UPDATE a.b o SET o.x = 01 WHERE true", line: 1, column: 24, message: "01 is not a number JSON reads"},
		{name: "invalid JSON", input: `UPDATE a.b o SET o.x = {"a" 1} WHERE true`, line: 1, column: 24, message: "not valid JSON"},
		{name: "a ) that closes the wrapper early", input: "UPDATE a.b o SET o.x = 1 WHERE o.a = 1) OR (true", line: 1, column: 39, message: "unbalanced parentheses in the WHERE"},
		{name: "a ( never closed", input: "UPDATE a.b o SET o.x = 1 WHERE (o.a = 1 OR o.b = 2", line: 1, column: 50, message: "unbalanced parentheses in the WHERE"},
		{name: "a ( never closed before a semicolon", input: "UPDATE a.b o SET o.x = 1 WHERE (o.a = 1;", line: 1, column: 40, message: "unbalanced parentheses in the WHERE"},
		{name: "UNSET the whole item", input: "UPDATE a.b o UNSET o WHERE true", line: 1, column: 20, message: "UNSET needs a field"},
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

func TestIsMutation(t *testing.T) {
	assert.True(t, query.IsMutation(goalUpdate))
	assert.True(t, query.IsMutation("-- note\nupdate a.b"))
	assert.True(t, query.IsMutation("WITH x AS (SELECT 1) UPDATE a.b"))
	assert.False(t, query.IsMutation("SELECT c.update FROM c"))
	assert.False(t, query.IsMutation("WITH x AS (SELECT * FROM a.b) SELECT c.update FROM c"), "a field named update")
	assert.False(t, query.IsMutation("WITH x AS (SELECT VALUE u.update FROM a.b u) SELECT * FROM x"), "one inside a CTE")
	assert.True(t, query.IsMutation("WITH x AS (SELECT * FROM a.b) UPDATE a.b o SET o.x = 1 WHERE true"))
	for _, text := range append(append([]string{}, plannerSeeds...), batchSeeds...) {
		assert.False(t, query.IsMutation(text), text)
	}
}

func TestMutationTargetNamesEveryPart(t *testing.T) {
	assert.Equal(t, []string{"prod", "sales", "orders"}, query.MutationTarget("UPDATE prod.sales.orders o SET o.x = 1 WHERE true"))
	assert.Equal(t, []string{"sales", "orders"}, query.MutationTarget(goalUpdate))
	assert.Nil(t, query.MutationTarget("SELECT * FROM c"))
}

var mutationSeeds = []string{
	goalUpdate,
	`UPDATE a.b AS o SET o["x-y"].z[3] = {"a": [1, null]}, o.n = -0.5 UNSET o.tmp WHERE o.id IN ("a", "b");`,
	"UPDATE a.b o UNSET o.tmp WHERE true",
	"UPDATE a.b o SET o.x = 'it\\'s' WHERE o.note = \"x;y\" -- tail",
	"UPDATE a.b",
	"UPDATE a.b o SET",
	"UPDATE a.b o SET o.x = ",
}

func FuzzParseMutation(f *testing.F) {
	for _, seed := range append(append([]string{}, mutationSeeds...), plannerSeeds...) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		parsed, err := query.ParseMutation(input)
		if err != nil {
			return
		}
		depth := 0
		for _, r := range stripStrings(parsed.Where) {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
			}
			require.GreaterOrEqual(t, depth, 0, "a ) closes the wrapper early: %s", parsed.Where)
		}
		require.Zero(t, depth, "the WHERE's parentheses pair up: %s", parsed.Where)
		text := parsed.String()
		again, err := query.ParseMutation(text)
		require.NoError(t, err, text)
		require.Equal(t, parsed, again, text)
		require.True(t, query.IsMutation(input))
	})
}

func TestAPointerOfEveryStep(t *testing.T) {
	path := query.FieldPath{Alias: "o", Steps: []query.PathStep{{Name: "a"}, {Index: 2, IsIndex: true}, {Name: "b c"}}}

	assert.Equal(t, "/a/2/b c", path.Pointer())
	assert.Equal(t, `o.a[2]["b c"]`, path.String())
}

func TestTheStatementFormatsBackToItself(t *testing.T) {
	m := parseMutation(t, goalUpdate)

	assert.Equal(t, `UPDATE sales.orders AS o SET o.status = "archived", o.archivedAt = "2026-01-01" WHERE o.status = "shipped" AND o.total < 50`, m.String())
	assert.Equal(t, json.RawMessage(`"archived"`), parseMutation(t, m.String()).Assignments[0].Value)
}

// stripStrings blanks the string literals and comments of a condition,
// whose parentheses are text rather than grouping.
func stripStrings(condition string) string {
	var out []rune
	var quote rune
	escaped, comment := false, false
	runes := []rune(condition)
	for i, r := range runes {
		switch {
		case comment:
			comment = r != '\n'
			continue
		case quote != 0 && escaped:
			escaped = false
			continue
		case quote != 0 && r == '\\':
			escaped = true
			continue
		case quote != 0 && r == quote:
			quote = 0
			continue
		case quote != 0:
			continue
		case r == '"' || r == '\'':
			quote = r
			continue
		case r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			comment = true
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
