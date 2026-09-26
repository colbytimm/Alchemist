package query_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

// flagged pairs a diagnostic's message with the text it covers.
type flagged struct {
	text    string
	message string
}

func diagnose(input string) []flagged {
	var found []flagged
	for _, d := range query.Diagnose(query.Analyze(input)) {
		found = append(found, flagged{text: input[d.Start:d.End], message: d.Message})
	}
	return found
}

func TestDiagnoseFlags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []flagged
	}{
		{
			name:  "an unterminated string",
			input: `SELECT * FROM c WHERE c.region = "west`,
			want:  []flagged{{`"west`, `unterminated string: close it with "`}},
		},
		{
			name:  "a character Cosmos SQL does not use",
			input: "SELECT * FROM c WHERE c.total # 5",
			want:  []flagged{{"#", `"#" is not part of Cosmos SQL`}},
		},
		{
			name:  "an unknown function, with the one it is near",
			input: `SELECT * FROM c WHERE CONTAIN(c.name, "A")`,
			want:  []flagged{{"CONTAIN", "unknown function CONTAIN: did you mean CONTAINS?"}},
		},
		{
			name:  "an unknown function near none",
			input: "SELECT * FROM c WHERE WOMBAT(c.name)",
			want:  []flagged{{"WOMBAT", "unknown function WOMBAT"}},
		},
		{
			name:  "an alias the query never declares",
			input: "SELECT o.id FROM c",
			want:  []flagged{{"o", "o is not declared: the query reads c"}},
		},
		{
			name:  "a misspelled clause keyword in a clause position",
			input: "SELECT * FORM c",
			want:  []flagged{{"FORM", "FORM is not a clause: did you mean FROM?"}},
		},
		{
			name:  "a misspelled clause taken for an alias nothing reads through",
			input: "SELECT * FROM c WERE c.total > 5",
			want:  []flagged{{"WERE", "WERE is not a clause: did you mean WHERE?"}},
		},
		{
			name:  "a statement that does not start with a statement keyword",
			input: "SELEC * FROM c",
			want:  []flagged{{"SELEC", "a query starts with SELECT"}},
		},
		{
			name:  "an unclosed bracket",
			input: "SELECT * FROM c WHERE (c.a = 1",
			want:  []flagged{{"(", "( is never closed"}},
		},
		{
			name:  "a closing bracket nothing opened",
			input: "SELECT * FROM c WHERE c.a = 1)",
			want:  []flagged{{")", ") has no opening ("}},
		},
		{
			name:  "a malformed batch, at the token its parser stopped on",
			input: "BEGIN BATCH sales.orders PARTITION",
			want:  []flagged{{"PARTITION", "a partition key value is a string, a number, TRUE, FALSE or NULL"}},
		},
		{
			name:  "a malformed batch, mid statement",
			input: "BEGIN BATCH sales.orders PARTITION 'k'; FETCH 'a'; COMMIT",
			want:  []flagged{{"FETCH", "expected CREATE, UPSERT, REPLACE, DELETE, READ, PATCH or COMMIT"}},
		},
		{
			name:  "several problems, in the order they appear",
			input: "SELECT * FORM c WHERE CONTAIN(c.name, 'A')",
			want: []flagged{
				{"FORM", "FORM is not a clause: did you mean FROM?"},
				{"CONTAIN", "unknown function CONTAIN: did you mean CONTAINS?"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, diagnose(tt.input))
		})
	}
}

func TestDiagnoseLeavesAlone(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "an unknown field, since Cosmos has no schema", input: "SELECT c.unheard FROM c WHERE c.whatever = 1"},
		{name: "an unknown container", input: "SELECT * FROM missing.container"},
		{name: "a cross-container shape the planner refuses", input: "SELECT * FROM sales.orders o LEFT JOIN sales.customers cu ON o.cid = cu.id"},
		{name: "a function in any case", input: "SELECT * FROM c WHERE startswith(c.n, 'a') AND Array_Contains(c.t, 1)"},
		{name: "a user-defined function", input: "SELECT udf.discount(c.total) FROM c"},
		{name: "keywords before a parenthesis", input: "SELECT VALUE ARRAY(SELECT VALUE 1) FROM c WHERE EXISTS(SELECT 1) AND c.a IN (1, 2)"},
		{name: "the default alias of a named container", input: "SELECT c.id FROM sales.orders"},
		{name: "a join side read by its container's name", input: "SELECT orders.id FROM sales.orders JOIN sales.customers ON orders.cid = customers.id"},
		{name: "an element alias and the container under FROM IN", input: "SELECT t.name FROM t IN c.tags JOIN x IN t.kids WHERE x.n > 1"},
		{name: "an alias read through a bracket", input: `SELECT c["order-id"] FROM c`},
		{name: "a query typed down to its FROM", input: "SELECT c.id, c.name"},
		{name: "a batch being typed", input: "BEGIN"},
		{name: "a complete batch", input: `BEGIN BATCH sales.orders PARTITION 'k'; UPSERT {"id": "a"}; COMMIT`},
		{name: "a word that is near no clause", input: "SELECT * FROM c WHERE c.a = 1 banana"},
		{name: "strings and comments hide what is in them", input: "SELECT * FROM c -- FORM # CONTAIN(\nWHERE c.a = 'FORM # ('"},
		{name: "empty", input: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Empty(t, diagnose(tt.input))
		})
	}
}

func TestDidYouMeanOffersOnlyWordsWithinTwoEdits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "one edit", input: "SELECT * FROM c WHERE UPPERS(c.n) = 'A'", want: "unknown function UPPERS: did you mean UPPER?"},
		{name: "two edits", input: "SELECT * FROM c WHERE ROUNDED(c.n) = 1", want: "unknown function ROUNDED: did you mean ROUND?"},
		{name: "three edits", input: "SELECT * FROM c WHERE ROUNDING(c.n) = 1", want: "unknown function ROUNDING"},
		{name: "a clause one edit away", input: "SELECT * FROM c ORDERS BY c.n", want: "ORDERS is not a clause: did you mean ORDER?"},
		{name: "a clause three edits away", input: "SELECT * FROM c WHERE c.a = 1 OBD BY c.n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var messages []string
			for _, d := range diagnose(tt.input) {
				messages = append(messages, d.message)
			}
			if tt.want == "" {
				require.Empty(t, messages)
				return
			}
			require.Equal(t, []string{tt.want}, messages)
		})
	}
}

func TestLexicalDiagnosticsAreTheOnesALexDecides(t *testing.T) {
	analysis := query.Analyze("SELECT * FORM c WHERE c.a # 'open")

	var found []string
	for _, d := range analysis.LexicalDiagnostics() {
		found = append(found, d.Message)
	}

	require.ElementsMatch(t, []string{`"#" is not part of Cosmos SQL`, "unterminated string: close it with '"}, found)
}

func FuzzDiagnose(f *testing.F) {
	for _, seed := range append(plannerSeeds, "SELECT * FORM c", "BEGIN BATCH a.b PARTITION", "é(é", "#`\\", "((]]}") {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var ranges []byteRange
		for _, d := range query.Diagnose(query.Analyze(input)) {
			require.NotEmpty(t, d.Message)
			ranges = append(ranges, byteRange{d.Start, d.End})
		}
		requireRangesInside(t, input, ranges)
	})
}

// BenchmarkDiagnose checks a query of 2,000 lines, more than anyone types
// by hand, against its budget of a third of a 60 Hz frame.
func BenchmarkDiagnose(b *testing.B) {
	lines := make([]string, 0, 2000)
	lines = append(lines, "SELECT o.id, CONTAINS(o.name, 'a') FROM sales.orders o JOIN t IN o.tags")
	for i := range 1999 {
		lines = append(lines, fmt.Sprintf("  WHERE o.n = %d AND STARTSWITH(t.label, \"x\") OR o.total > @min", i))
	}
	analysis := query.Analyze(strings.Join(lines, "\n"))
	b.ResetTimer()
	for range b.N {
		query.Diagnose(analysis)
	}
}
