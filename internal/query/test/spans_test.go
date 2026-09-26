package query_test

// cspell:ignore müller dirección calle größe größ WHER WHRE WHEER ordr

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

// wordTexts are spanTexts without punctuation, for cases about words.
func wordTexts(input string) []spanText {
	var texts []spanText
	for _, span := range spanTexts(input) {
		if span.kind != query.SpanPunctuation {
			texts = append(texts, span)
		}
	}
	return texts
}

func keyword(text string) spanText     { return spanText{query.SpanKeyword, text} }
func operator(text string) spanText    { return spanText{query.SpanOperator, text} }
func literal(text string) spanText     { return spanText{query.SpanLiteral, text} }
func function(text string) spanText    { return spanText{query.SpanFunction, text} }
func alias(text string) spanText       { return spanText{query.SpanAlias, text} }
func parameter(text string) spanText   { return spanText{query.SpanParameter, text} }
func str(text string) spanText         { return spanText{query.SpanString, text} }
func number(text string) spanText      { return spanText{query.SpanNumber, text} }
func comment(text string) spanText     { return spanText{query.SpanComment, text} }
func punctuation(text string) spanText { return spanText{query.SpanPunctuation, text} }

func TestSpansClassifiesWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []spanText
	}{
		{
			name:  "clause keywords in any case",
			input: "select DISTINCT VALUE From c",
			want:  []spanText{keyword("select"), keyword("DISTINCT"), keyword("VALUE"), keyword("From"), alias("c")},
		},
		{
			name:  "operators apart from clauses",
			input: "WHERE NOT c.a IN (1) AND c.b LIKE 'x%' ESCAPE '!' OR EXISTS(SELECT VALUE 1) OR c.n BETWEEN 1 AND 2",
			want: []spanText{
				keyword("WHERE"), operator("NOT"), operator("IN"), number("1"), operator("AND"), operator("LIKE"),
				str("'x%'"), operator("ESCAPE"), str("'!'"), operator("OR"), operator("EXISTS"), keyword("SELECT"),
				keyword("VALUE"), number("1"), operator("OR"), operator("BETWEEN"), number("1"), operator("AND"), number("2"),
			},
		},
		{
			name:  "literals",
			input: "c.a = true OR c.b = NULL OR c.c = undefined OR c.d = FALSE",
			want: []spanText{
				literal("true"), operator("OR"), literal("NULL"), operator("OR"), literal("undefined"), operator("OR"), literal("FALSE"),
			},
		},
		{
			name:  "a keyword naming a property is not one",
			input: "SELECT c.value, c . order FROM c",
			want:  []spanText{keyword("SELECT"), alias("c"), alias("c"), keyword("FROM"), alias("c")},
		},
		{
			name:  "a keyword inside an identifier is not one",
			input: "SELECT c.fromage, c.top_score FROM c",
			want:  []spanText{keyword("SELECT"), alias("c"), alias("c"), keyword("FROM"), alias("c")},
		},
		{
			name:  "a function only when a parenthesis follows",
			input: "SELECT COUNT(1), c.count, StartsWith (c.n, 'a') FROM c",
			want: []spanText{
				keyword("SELECT"), function("COUNT"), number("1"), alias("c"), function("StartsWith"),
				alias("c"), str("'a'"), keyword("FROM"), alias("c"),
			},
		},
		{
			name:  "an unknown name before a parenthesis is no function",
			input: "SELECT CONTAIN(c.n) FROM c",
			want:  []spanText{keyword("SELECT"), alias("c"), keyword("FROM"), alias("c")},
		},
		{
			name:  "a user-defined function",
			input: "SELECT udf.discount(c.total) FROM c",
			want:  []spanText{keyword("SELECT"), keyword("udf"), function("discount"), alias("c"), keyword("FROM"), alias("c")},
		},
		{
			name:  "aliases declared by AS, a bare name, JOIN and IN",
			input: "SELECT o.id, cu.name, t FROM sales.orders AS o JOIN sales.customers cu ON o.cid = cu.id JOIN t IN o.tags",
			want: []spanText{
				keyword("SELECT"), alias("o"), alias("cu"), keyword("FROM"), keyword("AS"), alias("o"),
				keyword("JOIN"), alias("cu"), keyword("ON"), alias("o"), alias("cu"),
				keyword("JOIN"), alias("t"), operator("IN"), alias("o"),
			},
		},
		{
			name:  "an alias read through a bracket",
			input: `SELECT c["order-id"] FROM c`,
			want:  []spanText{keyword("SELECT"), alias("c"), str(`"order-id"`), keyword("FROM"), alias("c")},
		},
		{
			name:  "a name matching an alias is not one where it reads nothing",
			input: "SELECT c.o, 'o' FROM c JOIN o IN c.items",
			want: []spanText{
				keyword("SELECT"), alias("c"), str("'o'"), keyword("FROM"), alias("c"),
				keyword("JOIN"), alias("o"), operator("IN"), alias("c"),
			},
		},
		{
			name:  "the container a FROM IN ranges over is no alias",
			input: "SELECT t FROM t IN c.tags",
			want:  []spanText{keyword("SELECT"), keyword("FROM"), alias("t"), operator("IN")},
		},
		{
			name:  "parameters",
			input: "WHERE c.total > @minTotal AND c.n = @ gap",
			want:  []spanText{keyword("WHERE"), parameter("@minTotal"), operator("AND")},
		},
		{
			name:  "strings in either quote, keywords inside them left alone",
			input: `WHERE c.a = 'select "x"' OR c.b = "it's"`,
			want:  []spanText{keyword("WHERE"), str(`'select "x"'`), operator("OR"), str(`"it's"`)},
		},
		{
			name:  "an escaped quote does not end the string",
			input: `'it\'s' AND`,
			want:  []spanText{str(`'it\'s'`), operator("AND")},
		},
		{
			name:  "an unterminated string runs to the end",
			input: "WHERE c.a = 'open AND",
			want:  []spanText{keyword("WHERE"), str("'open AND")},
		},
		{
			name:  "integers, decimals and exponents",
			input: "TOP 5 WHERE c.n > 1.5 AND c.m < 2e-3",
			want:  []spanText{keyword("TOP"), number("5"), keyword("WHERE"), number("1.5"), operator("AND"), number("2e-3")},
		},
		{
			name:  "a digit inside an identifier is not a number",
			input: "c.line2 = 7",
			want:  []spanText{number("7")},
		},
		{
			name:  "a number keeps no trailing dot or dangling exponent",
			input: "1. 2e 3e+",
			want:  []spanText{number("1"), number("2"), number("3")},
		},
		{
			name:  "a comment runs to the end of its line and hides its quotes",
			input: "SELECT * -- the customer's 'orders'\nFROM c",
			want:  []spanText{keyword("SELECT"), comment("-- the customer's 'orders'"), keyword("FROM"), alias("c")},
		},
		{
			name:  "a comment at the end runs out with the input",
			input: "SELECT 1 --",
			want:  []spanText{keyword("SELECT"), number("1"), comment("--")},
		},
		{
			name:  "a non-ASCII name is one word, and a non-ASCII space separates words",
			input: "SELECT\u00a0c.müller FROM\u3000c WHERE c.größe > 1",
			want:  []spanText{keyword("SELECT"), alias("c"), keyword("FROM"), alias("c"), keyword("WHERE"), alias("c"), number("1")},
		},
		{
			name:  "a parameter's property",
			input: "WHERE c.s = @filter.status",
			want:  []spanText{keyword("WHERE"), parameter("@filter")},
		},
		{name: "empty input", input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, wordTexts(tt.input))
		})
	}
}

func TestSpansMarkPunctuationAndLeaveStrayCharactersPlain(t *testing.T) {
	require.Equal(t, []spanText{
		punctuation("("), punctuation("."), punctuation("["), number("0"), punctuation("]"),
		punctuation("!"), punctuation("="), number("1"), punctuation("?"), punctuation("?"), punctuation("|"),
		punctuation("|"), punctuation(","), punctuation(")"), number("5"),
	}, spanTexts("(c.[0] != 1 ?? || , ) # ` \\ 5 é"))
}

func TestBatchWordsAreKeywordsOnlyInABatch(t *testing.T) {
	batch := `begin BATCH sales.read PARTITION 1; READ "a"; PATCH "b" [] WHERE "x" IF MATCH "e"; UPSERT {"ok": true}; COMMIT`

	require.Equal(t, []spanText{
		keyword("begin"), keyword("BATCH"), keyword("PARTITION"), number("1"), keyword("READ"), str(`"a"`),
		keyword("PATCH"), str(`"b"`), keyword("WHERE"), str(`"x"`), keyword("IF"), keyword("MATCH"), str(`"e"`),
		keyword("UPSERT"), str(`"ok"`), literal("true"), keyword("COMMIT"),
	}, wordTexts(batch))
	require.Equal(t, []spanText{keyword("SELECT"), keyword("FROM"), alias("c")},
		wordTexts("SELECT read FROM c"), "READ is a name outside a batch")
	require.Equal(t, []spanText{keyword("SELECT"), function("REPLACE"), alias("c"), str("'a'"), str("'b'"), keyword("FROM"), alias("c")},
		wordTexts("SELECT REPLACE(c.n, 'a', 'b') FROM c"), "and REPLACE a function")
}

func TestAnalysisSharesOneParseBetweenSpansAndCompletion(t *testing.T) {
	text := "SELECT * FROM c WHERE c."
	analysis := query.Analyze(text)

	require.Equal(t, query.Spans(text), analysis.Spans())
	require.Equal(t, query.Context(text, len(text)), analysis.Context(len(text)))
}

func FuzzSpans(f *testing.F) {
	for _, seed := range append(plannerSeeds, `'\`, "1e", "9.", "c.a = 'é' AND 2e+", "\x00'\\", "@p@q", "é(") {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		requireRangesInside(t, input, spanRanges(query.Spans(input)))
	})
}

type byteRange struct{ start, end int }

func spanRanges(spans []query.Span) []byteRange {
	ranges := make([]byteRange, 0, len(spans))
	for _, span := range spans {
		ranges = append(ranges, byteRange{span.Start, span.End})
	}
	return ranges
}

// requireRangesInside checks that ranges lie inside input, in order, apart,
// and on rune boundaries.
func requireRangesInside(t *testing.T, input string, ranges []byteRange) {
	t.Helper()
	end := 0
	for _, r := range ranges {
		require.GreaterOrEqual(t, r.start, end, "in order and apart")
		require.Greater(t, r.end, r.start)
		require.LessOrEqual(t, r.end, len(input))
		require.True(t, onRuneBoundary(input, r.start) && onRuneBoundary(input, r.end), "on rune boundaries")
		end = r.end
	}
}

// onRuneBoundary reports whether i is where ranging over s starts a rune,
// which for text that is not valid UTF-8 includes each stray byte.
func onRuneBoundary(s string, i int) bool {
	for at := range s {
		if at >= i {
			return at == i
		}
	}
	return i == len(s)
}

func TestUpdateKeywordsAreKeywordsOnlyInAnUpdate(t *testing.T) {
	update := `UPDATE sales.orders o SET o.set = 1 UNSET o.x WHERE o.y = "a" AND STARTSWITH(o.z, @p) OR true`

	require.Equal(t, []spanText{
		keyword("UPDATE"), alias("o"), keyword("SET"), alias("o"), number("1"), keyword("UNSET"), alias("o"),
		keyword("WHERE"), alias("o"), str(`"a"`), operator("AND"), function("STARTSWITH"), alias("o"),
		parameter("@p"), operator("OR"), literal("true"),
	}, wordTexts(update))
	require.Equal(t, []spanText{keyword("SELECT"), keyword("FROM"), alias("c")},
		wordTexts("SELECT set FROM c"), "SET is a name outside an update")
}

func TestAnUpdateWithoutAnAliasReadsThroughTheDefault(t *testing.T) {
	require.Equal(t, []spanText{keyword("UPDATE"), keyword("SET"), alias("c"), number("1"), keyword("WHERE"), literal("true")},
		wordTexts("UPDATE sales.orders SET c.n = 1 WHERE true"))
}

func TestEndsNameReadsNamesAsTheLexerDoes(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "an ASCII name", text: "SELECT c.tot", want: true},
		{name: "a non-ASCII name", text: "SELECT c.größ", want: true},
		{name: "a dot", text: "SELECT c.", want: true},
		{name: "a number", text: "WHERE c.n = 12", want: false},
		{name: "a space", text: "SELECT ", want: false},
		{name: "a no-break space", text: "SELECT ", want: false},
		{name: "nothing", text: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, query.EndsName(tt.text))
		})
	}
}
