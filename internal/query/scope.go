// Package query reads editor queries without executing them. It detects
// `FROM <db>.<container>` references, rewrites them to an adapter-native
// alias, and reports the scope so the caller can route the query to the right
// container; and it marks the spans of a query worth highlighting.
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
	p := parser{toks: lex(text), aliases: map[string]bool{}}
	p.run()
	var cands []source
	for _, s := range p.sources {
		if len(s.path) == 2 && !p.aliases[s.path[0].text] {
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
	tokNumber
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
		case isDigit(c):
			end := lexNumber(s, i)
			toks = append(toks, token{kind: tokNumber, start: i, end: end})
			i = end
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

// lexNumber returns the byte offset just past the number opening at i. A
// fraction or an exponent counts only once a digit follows it, so the dot of
// "1." and the e of "2e" are left for the next token.
func lexNumber(s string, i int) int {
	i = skipDigits(s, i)
	if i+1 < len(s) && s[i] == '.' && isDigit(s[i+1]) {
		i = skipDigits(s, i+1)
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		exponent := i + 1
		if exponent < len(s) && (s[exponent] == '+' || s[exponent] == '-') {
			exponent++
		}
		if exponent < len(s) && isDigit(s[exponent]) {
			i = skipDigits(s, exponent)
		}
	}
	return i
}

func skipDigits(s string, i int) int {
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isIdentStart reports whether c can begin an identifier.
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIdentPart reports whether c can continue an identifier.
func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// parser walks a lexed query for FROM-clause sources. Every parse method takes
// the index to read from and returns the index just past what it consumed.
type parser struct {
	toks    []token
	sources []source
	aliases map[string]bool
}

func (p *parser) run() {
	for i := 0; i < len(p.toks); {
		if p.isKeyword(i, "FROM") {
			i = p.parseFromClause(i + 1)
			continue
		}
		i++
	}
}

func (p *parser) parseFromClause(i int) int {
	for {
		src, j, ok := p.parseSource(i)
		if !ok {
			return j
		}
		p.record(src)
		i = j
		for p.isKeyword(i, "JOIN") {
			i = p.parseJoinClause(i + 1)
		}
		if !p.isComma(i) {
			return i
		}
		i++
	}
}

// parseJoinClause binds the alias of `JOIN alias IN collection` and skips its
// path; a bare `JOIN path` is recorded as a source so cross-container
// references are detected.
func (p *parser) parseJoinClause(i int) int {
	src, j, ok := p.parseSource(i)
	if !ok {
		return j
	}
	if !p.isKeyword(j, "IN") {
		p.record(src)
		return j
	}
	if len(src.path) == 1 && src.alias == "" {
		p.aliases[src.path[0].text] = true
	}
	if _, k, ok := p.parseSource(j + 1); ok {
		return k
	}
	return j + 1
}

// parseSource consumes one dotted path plus an optional `AS alias` or bare
// alias.
func (p *parser) parseSource(i int) (source, int, bool) {
	if i >= len(p.toks) || p.toks[i].kind != tokIdent || keywords[p.toks[i].upper] {
		return source{}, i, false
	}
	src := source{path: []token{p.toks[i]}, start: p.toks[i].start, end: p.toks[i].end}
	i++
	for i+1 < len(p.toks) && p.toks[i].kind == tokDot && p.toks[i+1].kind == tokIdent {
		src.path = append(src.path, p.toks[i+1])
		src.end = p.toks[i+1].end
		i += 2
	}
	switch {
	case i+1 < len(p.toks) && p.isKeyword(i, "AS") && p.toks[i+1].kind == tokIdent:
		src.alias = p.toks[i+1].text
		src.end = p.toks[i+1].end
		i += 2
	case i < len(p.toks) && p.toks[i].kind == tokIdent && !keywords[p.toks[i].upper]:
		src.alias = p.toks[i].text
		src.end = p.toks[i].end
		i++
	}
	return src, i, true
}

func (p *parser) record(src source) {
	p.sources = append(p.sources, src)
	if src.alias != "" {
		p.aliases[src.alias] = true
	}
}

func (p *parser) isKeyword(i int, kw string) bool {
	return i < len(p.toks) && p.toks[i].kind == tokIdent && p.toks[i].upper == kw
}

func (p *parser) isComma(i int) bool {
	return i < len(p.toks) && p.toks[i].kind == tokComma
}
