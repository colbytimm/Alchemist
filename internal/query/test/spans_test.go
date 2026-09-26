package query_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/query"
)

// spanText pairs each span's kind with the text it covers, which reads
// better in a failure than byte offsets do.
type spanText struct {
	kind query.SpanKind
	text string
}

func spanTexts(input string) []spanText {
	var texts []spanText
	for _, span := range query.Spans(input) {
		texts = append(texts, spanText{kind: span.Kind, text: input[span.Start:span.End]})
	}
	return texts
}

func TestSpans(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []spanText
	}{
		{
			name:  "cosmos keywords in any case",
			input: "select VALUE t From c INNER JOIN t IN c.tags",
			want: []spanText{
				{query.SpanKeyword, "select"}, {query.SpanKeyword, "VALUE"}, {query.SpanKeyword, "From"},
				{query.SpanKeyword, "INNER"}, {query.SpanKeyword, "JOIN"}, {query.SpanKeyword, "IN"},
			},
		},
		{
			name:  "a keyword inside an identifier is not one",
			input: "SELECT c.fromage, c.top_score FROM c",
			want:  []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanKeyword, "FROM"}},
		},
		{
			name:  "a keyword naming a property is not one",
			input: "SELECT c.value, c . order FROM c",
			want:  []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanKeyword, "FROM"}},
		},
		{
			name:  "strings in either quote, keywords inside them left alone",
			input: `WHERE c.a = 'select "x"' OR c.b = "it's"`,
			want: []spanText{
				{query.SpanKeyword, "WHERE"}, {query.SpanString, `'select "x"'`},
				{query.SpanKeyword, "OR"}, {query.SpanString, `"it's"`},
			},
		},
		{
			name:  "an escaped quote does not end the string",
			input: `'it\'s' AND`,
			want:  []spanText{{query.SpanString, `'it\'s'`}, {query.SpanKeyword, "AND"}},
		},
		{
			name:  "an unterminated string runs to the end",
			input: "WHERE c.a = 'open AND",
			want:  []spanText{{query.SpanKeyword, "WHERE"}, {query.SpanString, "'open AND"}},
		},
		{
			name:  "integers, decimals and exponents",
			input: "TOP 5 WHERE c.n > 1.5 AND c.m < 2e-3",
			want: []spanText{
				{query.SpanKeyword, "TOP"}, {query.SpanNumber, "5"}, {query.SpanKeyword, "WHERE"},
				{query.SpanNumber, "1.5"}, {query.SpanKeyword, "AND"}, {query.SpanNumber, "2e-3"},
			},
		},
		{
			name:  "a digit inside an identifier is not a number",
			input: "c.line2 = 7",
			want:  []spanText{{query.SpanNumber, "7"}},
		},
		{
			name:  "a number keeps no trailing dot or dangling exponent",
			input: "1. 2e 3e+",
			want:  []spanText{{query.SpanNumber, "1"}, {query.SpanNumber, "2"}, {query.SpanNumber, "3"}},
		},
		{
			name:  "a comment runs to the end of its line and hides its quotes",
			input: "SELECT * -- the customer's 'orders'\nFROM c",
			want: []spanText{
				{query.SpanKeyword, "SELECT"}, {query.SpanComment, "-- the customer's 'orders'"},
				{query.SpanKeyword, "FROM"},
			},
		},
		{
			name:  "a comment at the end runs out with the input",
			input: "SELECT 1 --",
			want:  []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanNumber, "1"}, {query.SpanComment, "--"}},
		},
		{
			name:  "a lone dash is not a comment",
			input: "c.a - 1",
			want:  []spanText{{query.SpanNumber, "1"}},
		},
		{name: "empty input", input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, spanTexts(tt.input))
		})
	}
}

func FuzzSpansStayInsideTheInputAndInOrder(f *testing.F) {
	for _, seed := range []string{
		"SELECT * FROM c", `'\`, `"unterminated`, "1e", "9.", "c.a = 'é' AND 2e+", "\x00'\\",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		end := 0
		for _, span := range query.Spans(input) {
			require.GreaterOrEqual(t, span.Start, end)
			require.Greater(t, span.End, span.Start)
			require.LessOrEqual(t, span.End, len(input))
			end = span.End
		}
	})
}

func TestBatchKeywordsAreKeywordsOnlyInABatch(t *testing.T) {
	batch := `begin BATCH sales.read PARTITION 1; READ "a"; PATCH "b" [] WHERE "x" IF MATCH "e"; COMMIT`

	require.Equal(t, []spanText{
		{query.SpanKeyword, "begin"}, {query.SpanKeyword, "BATCH"}, {query.SpanKeyword, "PARTITION"},
		{query.SpanNumber, "1"}, {query.SpanKeyword, "READ"}, {query.SpanString, `"a"`},
		{query.SpanKeyword, "PATCH"}, {query.SpanString, `"b"`}, {query.SpanKeyword, "WHERE"}, {query.SpanString, `"x"`},
		{query.SpanKeyword, "IF"}, {query.SpanKeyword, "MATCH"}, {query.SpanString, `"e"`}, {query.SpanKeyword, "COMMIT"},
	}, spanTexts(batch))
	require.Equal(t, []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanKeyword, "FROM"}},
		spanTexts("SELECT read FROM c"), "READ is an alias outside a batch")
}

func TestUpdateKeywordsAreKeywordsOnlyInAnUpdate(t *testing.T) {
	update := `UPDATE sales.orders o SET o.set = 1 UNSET o.x WHERE o.y = "a" AND true`

	require.Equal(t, []spanText{
		{query.SpanKeyword, "UPDATE"}, {query.SpanKeyword, "SET"}, {query.SpanNumber, "1"}, {query.SpanKeyword, "UNSET"},
		{query.SpanKeyword, "WHERE"}, {query.SpanString, `"a"`}, {query.SpanKeyword, "AND"}, {query.SpanKeyword, "true"},
	}, spanTexts(update))
	require.Equal(t, []spanText{{query.SpanKeyword, "SELECT"}, {query.SpanKeyword, "FROM"}},
		spanTexts("SELECT set FROM c"), "SET is an alias outside an update")
}
