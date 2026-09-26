package query

import (
	"maps"
	"slices"
	"strings"
)

type SpanKind int

const (
	SpanKeyword SpanKind = iota + 1
	SpanString
	SpanNumber
	SpanComment
	SpanOperator
	SpanLiteral
	SpanFunction
	SpanAlias
	SpanParameter
	SpanPunctuation
)

// Span is a byte range of a query worth telling apart from the text around it.
type Span struct {
	Kind  SpanKind
	Start int
	End   int
}

// vocabulary is the words one kind of statement highlights. Anything else,
// a property or a container name, stays plain text.
type vocabulary struct {
	keywords  map[string]bool
	operators map[string]bool
	literals  map[string]bool
	// names marks a statement that calls functions and reads through
	// aliases, which a batch does neither of.
	names bool
}

var (
	literalWords  = map[string]bool{"TRUE": true, "FALSE": true, "NULL": true, "UNDEFINED": true}
	operatorWords = map[string]bool{
		"AND": true, "OR": true, "NOT": true, "IN": true, "LIKE": true, "BETWEEN": true,
		"ESCAPE": true, "EXISTS": true, "ARRAY": true, "IS": true,
	}
	queryVocabulary    = vocabulary{keywords: queryKeywords(), operators: operatorWords, literals: literalWords, names: true}
	mutationVocabulary = vocabulary{
		keywords:  withKeys(queryVocabulary.keywords, slices.Collect(maps.Keys(mutationKeywords))...),
		operators: operatorWords, literals: literalWords, names: true,
	}
	batchVocabulary = vocabulary{keywords: batchKeywords, literals: literalWords}
)

// queryKeywords are the words of a query that neither operate on values nor
// are values: its clauses, and the words a join is written with.
func queryKeywords() map[string]bool {
	words := map[string]bool{}
	for _, set := range []map[string]bool{keywords, joinModifiers} {
		for word := range set {
			if !operatorWords[word] && !literalWords[word] {
				words[word] = true
			}
		}
	}
	return words
}

// batchKeywords are the words of a batch statement. They are a set of their
// own rather than part of keywords, whose rule is that none can be a bare
// source alias: READ and MATCH can.
var batchKeywords = map[string]bool{
	"BEGIN": true, "BATCH": true, "PARTITION": true, "COMMIT": true,
	"CREATE": true, "UPSERT": true, "REPLACE": true, "DELETE": true, "READ": true, "PATCH": true,
	"WHERE": true, "IF": true, "MATCH": true,
}

// mutationKeywords are the words an UPDATE adds to a query's, kept apart
// for batchKeywords' reason: SET can be an alias.
var mutationKeywords = map[string]bool{"UPDATE": true, "SET": true, "UNSET": true}

// punctuation are the symbols Cosmos SQL is written with, the batch
// grammar's included.
const punctuation = "()[]{}=!<>+-*/%?|&^~:;"

func Spans(text string) []Span {
	return Analyze(text).Spans()
}

// Spans lists what the text is made of, in the order it appears. The
// statement decides the words: a batch's are its own, and an update's are a
// query's and its own.
func (a Analysis) Spans() []Span {
	s := spanner{Analysis: a, words: queryVocabulary, aliases: map[string]bool{}}
	switch {
	case a.batch:
		s.words = batchVocabulary
	case a.mutation:
		s.words = mutationVocabulary
	}
	for _, name := range a.aliasNames() {
		s.aliases[name] = true
	}
	return s.spans()
}

type spanner struct {
	Analysis
	words   vocabulary
	aliases map[string]bool
}

func (s spanner) spans() []Span {
	spans := make([]Span, 0, len(s.tokens))
	statement := 0
	for i := 0; i < len(s.tokens); i++ {
		tok := s.tokens[i]
		if tok.kind == tokComment {
			spans = append(spans, Span{Kind: SpanComment, Start: tok.start, End: tok.end})
			continue
		}
		if s.isParameter(statement) {
			name := s.code[statement+1]
			spans = append(spans, Span{Kind: SpanParameter, Start: tok.start, End: name.end})
			i, statement = i+1, statement+2
			continue
		}
		if kind, ok := s.spanKind(statement); ok {
			spans = append(spans, Span{Kind: kind, Start: tok.start, End: tok.end})
		}
		statement++
	}
	return spans
}

// isParameter reports whether an @ at i starts a parameter's name.
func (a Analysis) isParameter(i int) bool {
	return isSymbol(a.code[i], "@") && i+1 < len(a.code) &&
		a.code[i+1].kind == tokIdent && a.code[i+1].start == a.code[i].end
}

func (s spanner) spanKind(i int) (SpanKind, bool) {
	tok := s.code[i]
	switch tok.kind {
	case tokIdent:
		return s.identKind(i)
	case tokString:
		return SpanString, true
	case tokNumber:
		return SpanNumber, true
	case tokDot, tokComma:
		return SpanPunctuation, true
	}
	return SpanPunctuation, strings.Contains(punctuation, tok.text)
}

// identKind leaves a word after a dot alone, since c.value and c.order name
// properties, unless it names a user-defined function: udf.discount(.
func (s spanner) identKind(i int) (SpanKind, bool) {
	tok := s.code[i]
	switch {
	case followsDot(s.code, i):
		return SpanFunction, s.words.names && s.isUserFunction(i)
	case s.words.names && s.isCall(i) && builtinFunctions[tok.upper] != "":
		return SpanFunction, true
	case s.words.operators[tok.upper]:
		return SpanOperator, true
	case s.words.literals[tok.upper]:
		return SpanLiteral, true
	case s.words.keywords[tok.upper]:
		return SpanKeyword, true
	case s.words.names && s.isAlias(i):
		return SpanAlias, true
	}
	return 0, false
}

// isAlias reports whether the identifier at i declares an alias, or reads
// through one the query has.
func (s spanner) isAlias(i int) bool {
	return s.isDeclaration(i) || s.readsThrough(i) && !s.isRoot(i) && s.aliases[s.code[i].text]
}
