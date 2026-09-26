package query

import (
	"maps"
	"slices"
)

type SpanKind int

const (
	SpanKeyword SpanKind = iota + 1
	SpanString
	SpanNumber
	SpanComment
)

// Span is a byte range of a query worth telling apart from the text around it.
type Span struct {
	Kind  SpanKind
	Start int
	End   int
}

// batchKeywords are the words of a batch statement. They are a set of their
// own rather than part of keywords, whose rule is that none can be a bare
// source alias: READ and MATCH can.
var batchKeywords = map[string]bool{
	"BEGIN": true, "BATCH": true, "PARTITION": true, "COMMIT": true,
	"CREATE": true, "UPSERT": true, "REPLACE": true, "DELETE": true, "READ": true, "PATCH": true,
	"WHERE": true, "IF": true, "MATCH": true, "TRUE": true, "FALSE": true, "NULL": true,
}

// mutationKeywords are the words an UPDATE adds to a query's, kept apart
// for batchKeywords' reason: SET can be an alias.
var mutationKeywords = map[string]bool{"UPDATE": true, "SET": true, "UNSET": true}

// mutationSpanWords are what an update highlights: its condition is a
// query's.
var mutationSpanWords = withKeys(keywords, slices.Collect(maps.Keys(mutationKeywords))...)

// Spans lists the keywords, string literals, numbers, and comments of text in
// the order they appear. A batch statement's keywords are its own, and an
// update's are a query's and its own.
func Spans(text string) []Span {
	words := keywords
	switch {
	case IsBatch(text):
		words = batchKeywords
	case IsMutation(text):
		words = mutationSpanWords
	}
	var spans []Span
	previous := tokOther
	for _, tok := range lex(text) {
		if kind, ok := spanKind(tok, previous, words); ok {
			spans = append(spans, Span{Kind: kind, Start: tok.start, End: tok.end})
		}
		previous = tok.kind
	}
	return spans
}

// spanKind leaves a keyword that follows a dot alone: c.value and c.order
// name properties.
func spanKind(tok token, previous int, words map[string]bool) (SpanKind, bool) {
	switch {
	case tok.kind == tokIdent && words[tok.upper] && previous != tokDot:
		return SpanKeyword, true
	case tok.kind == tokString:
		return SpanString, true
	case tok.kind == tokNumber:
		return SpanNumber, true
	case tok.kind == tokComment:
		return SpanComment, true
	}
	return 0, false
}
