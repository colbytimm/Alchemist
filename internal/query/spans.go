package query

type SpanKind int

const (
	SpanKeyword SpanKind = iota + 1
	SpanString
	SpanNumber
)

// Span is a byte range of a query worth telling apart from the text around it.
type Span struct {
	Kind  SpanKind
	Start int
	End   int
}

// Spans lists the keywords, string literals, and numbers of text in the
// order they appear.
func Spans(text string) []Span {
	var spans []Span
	previous := tokOther
	for _, tok := range lex(text) {
		if kind, ok := spanKind(tok, previous); ok {
			spans = append(spans, Span{Kind: kind, Start: tok.start, End: tok.end})
		}
		previous = tok.kind
	}
	return spans
}

// spanKind leaves a keyword that follows a dot alone: c.value and c.order
// name properties.
func spanKind(tok token, previous int) (SpanKind, bool) {
	switch {
	case tok.kind == tokIdent && keywords[tok.upper] && previous != tokDot:
		return SpanKeyword, true
	case tok.kind == tokString:
		return SpanString, true
	case tok.kind == tokNumber:
		return SpanNumber, true
	}
	return 0, false
}
