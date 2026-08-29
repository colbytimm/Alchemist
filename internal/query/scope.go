// Package query parses editor queries for database/container scope. It
// detects `FROM <db>.<container>` references, rewrites them to an
// adapter-native alias, and reports the scope so the caller can route the
// query to the right container.
package query

import (
	"errors"
	"strings"
)

// ErrMultiContainer is returned when a query references more than one
// db.container source; cross-container execution arrives in iteration 10.
var ErrMultiContainer = errors.New("query: one container per query until iteration 10")

// Result is the outcome of parsing one editor query.
type Result struct {
	Rewritten string   // query text with any db.container source replaced by its alias
	Scope     []string // [database, container] when Explicit, else empty
	Explicit  bool     // true when the text named a db.container source
}

// ParseScope scans text for FROM-clause sources of the form db.container,
// rewrites the single source to its alias (or "c"), and returns the scope.
// Queries without such a source are returned unchanged. It never panics on
// arbitrary input.
func ParseScope(text string) (Result, error) {
	toks := lex(text)
	var sources []source
	aliases := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].upper == "FROM" {
			i = parseFromClause(toks, i+1, &sources, aliases) - 1
		}
	}
	var cands []source
	for _, s := range sources {
		if len(s.path) == 2 && !aliases[s.path[0].text] {
			cands = append(cands, s)
		}
	}
	switch len(cands) {
	case 0:
		return Result{Rewritten: text}, nil
	case 1:
		c := cands[0]
		alias := c.alias
		if alias == "" {
			alias = "c"
		}
		return Result{
			Rewritten: text[:c.start] + alias + text[c.end:],
			Scope:     []string{c.path[0].text, c.path[1].text},
			Explicit:  true,
		}, nil
	default:
		return Result{}, ErrMultiContainer
	}
}

// Token kinds produced by the lexer.
const (
	tokIdent = iota
	tokDot
	tokComma
	tokString
	tokOther
)

// token is one lexed unit with its byte span in the input.
type token struct {
	kind  int
	text  string // original ident text
	upper string // uppercased ident text, for keyword checks
	start int
	end   int
}

// keywords are idents that can never be a bare source alias.
var keywords = map[string]bool{
	"AND": true, "AS": true, "ASC": true, "BETWEEN": true, "BY": true,
	"CASE": true, "DESC": true, "DISTINCT": true, "ELSE": true, "END": true,
	"EXISTS": true, "FALSE": true, "FROM": true, "GROUP": true, "IN": true,
	"IS": true, "JOIN": true, "LIKE": true, "LIMIT": true, "NOT": true,
	"NULL": true, "OFFSET": true, "ON": true, "OR": true, "ORDER": true,
	"SELECT": true, "THEN": true, "TOP": true, "TRUE": true, "VALUE": true,
	"WHEN": true, "WHERE": true,
}

// source is one FROM-clause source: a dotted path plus an optional alias.
type source struct {
	path  []token
	alias string
	start int // byte offset of the first path token
	end   int // byte offset just past the last consumed token
}

// lex splits s into tokens, treating single- and double-quoted regions as
// opaque string literals (backslash escapes respected).
func lex(s string) []token {
	var toks []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '\'' || c == '"':
			end := lexString(s, i)
			toks = append(toks, token{kind: tokString, start: i, end: end})
			i = end
		case isIdentStart(c):
			start := i
			for i < len(s) && isIdentPart(s[i]) {
				i++
			}
			t := s[start:i]
			toks = append(toks, token{kind: tokIdent, text: t, upper: strings.ToUpper(t), start: start, end: i})
		case c == '.':
			toks = append(toks, token{kind: tokDot, start: i, end: i + 1})
			i++
		case c == ',':
			toks = append(toks, token{kind: tokComma, start: i, end: i + 1})
			i++
		default:
			toks = append(toks, token{kind: tokOther, start: i, end: i + 1})
			i++
		}
	}
	return toks
}

// lexString returns the byte offset just past the string literal opening at
// i; an unterminated literal consumes the rest of the input.
func lexString(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
		case quote:
			return i + 1
		default:
			i++
		}
	}
	return len(s)
}

// isIdentStart reports whether c can begin an identifier.
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIdentPart reports whether c can continue an identifier.
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// parseFromClause consumes the source list after a FROM keyword — sources
// separated by commas, plus JOIN clauses — and returns the index of the
// first token past the clause.
func parseFromClause(toks []token, i int, sources *[]source, aliases map[string]bool) int {
	for {
		src, j, ok := parseSource(toks, i, aliases)
		if !ok {
			return j
		}
		i = j
		*sources = append(*sources, src)
		if src.alias != "" {
			aliases[src.alias] = true
		}
		for {
			if i < len(toks) && toks[i].kind == tokComma {
				i++
				break // next comma-separated source
			}
			if i < len(toks) && toks[i].kind == tokIdent && toks[i].upper == "JOIN" {
				i = parseJoin(toks, i+1, sources, aliases)
				continue // a further JOIN or comma may follow
			}
			return i
		}
	}
}

// parseJoin consumes one JOIN clause. The `JOIN alias IN collection` form
// registers the alias and skips the collection path; a bare `JOIN path`
// records the path as a source so cross-container references are detected.
func parseJoin(toks []token, i int, sources *[]source, aliases map[string]bool) int {
	src, j, ok := parseSource(toks, i, aliases)
	if !ok {
		return j
	}
	if j < len(toks) && toks[j].kind == tokIdent && toks[j].upper == "IN" {
		if len(src.path) == 1 && src.alias == "" {
			aliases[src.path[0].text] = true
		}
		if _, j2, ok2 := parseSource(toks, j+1, aliases); ok2 {
			return j2
		}
		return j + 1
	}
	*sources = append(*sources, src)
	if src.alias != "" {
		aliases[src.alias] = true
	}
	return j
}

// parseSource consumes one dotted path plus an optional `AS alias` or bare
// alias, returning ok=false when no source starts at i.
func parseSource(toks []token, i int, _ map[string]bool) (source, int, bool) {
	if i >= len(toks) || toks[i].kind != tokIdent || keywords[toks[i].upper] {
		return source{}, i, false
	}
	src := source{path: []token{toks[i]}, start: toks[i].start, end: toks[i].end}
	i++
	for i+1 < len(toks) && toks[i].kind == tokDot && toks[i+1].kind == tokIdent {
		src.path = append(src.path, toks[i+1])
		src.end = toks[i+1].end
		i += 2
	}
	switch {
	case i+1 < len(toks) && toks[i].kind == tokIdent && toks[i].upper == "AS" && toks[i+1].kind == tokIdent:
		src.alias = toks[i+1].text
		src.end = toks[i+1].end
		i += 2
	case i < len(toks) && toks[i].kind == tokIdent && !keywords[toks[i].upper]:
		src.alias = toks[i].text
		src.end = toks[i].end
		i++
	}
	return src, i, true
}
