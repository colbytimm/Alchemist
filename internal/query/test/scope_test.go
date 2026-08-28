package query_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

func TestParseScopeTable(t *testing.T) {
	cases := []struct {
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
			scope:     nil,
			rewritten: "SELECT * FROM c",
		},
		{
			name:      "join over property path",
			input:     "SELECT t.name FROM c JOIN t IN c.tags",
			scope:     nil,
			rewritten: "SELECT t.name FROM c JOIN t IN c.tags",
		},
		{
			name:      "dotted pair inside string literal",
			input:     `SELECT * FROM c WHERE c.note = "FROM a.b"`,
			scope:     nil,
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
			scope:     nil,
			rewritten: "SELECT * FROM a.b.c.d",
		},
		{
			name:      "empty input",
			input:     "",
			scope:     nil,
			rewritten: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := query.ParseScope(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.rewritten, res.Rewritten)
			if len(tc.scope) == 0 {
				assert.Empty(t, res.Scope)
				assert.False(t, res.Explicit)
			} else {
				assert.Equal(t, tc.scope, res.Scope)
				assert.True(t, res.Explicit)
			}
		})
	}
}

func TestParseScopeRejectsMultipleContainers(t *testing.T) {
	for _, input := range []string{
		"SELECT * FROM a.b, x.y",
		"SELECT * FROM a.b JOIN x.y",
	} {
		_, err := query.ParseScope(input)
		require.ErrorIs(t, err, query.ErrMultiContainer, "input %q", input)
	}
}

func FuzzParseScope(f *testing.F) {
	for _, seed := range []string{
		"SELECT * FROM mydb.orders AS c WHERE c.total > 5",
		"SELECT * FROM c",
		"SELECT t.name FROM c JOIN t IN c.tags",
		`SELECT * FROM c WHERE c.note = "FROM a.b"`,
		"select * from MyDb.Orders as c",
		"FROM",
		"FROM a.",
		"FROM a.b AS",
		"'unterminated",
		`"also unterminated \`,
		"SELECT * FROM a.b, x.y",
		"SELECT * FROM (SELECT * FROM inner.things) outer",
		"..,,..''\"\"[[]]",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		res, err := query.ParseScope(input)
		if err != nil {
			return
		}
		if res.Explicit {
			assert.Len(t, res.Scope, 2)
		} else {
			assert.Equal(t, input, res.Rewritten)
		}
	})
}
