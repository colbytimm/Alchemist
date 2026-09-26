// Package query plans and executes editor queries above the adapter
// interfaces. A `FROM <db>.<container>` source is rewritten to an
// adapter-native alias and routed to its container; a query naming several
// containers is simulated client-side, as per-container queries merged
// locally. It also marks the spans of a query worth highlighting.
package query

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Token kinds produced by the lexer.
const (
	tokIdent = iota
	tokDot
	tokComma
	tokString
	tokNumber
	tokComment
	tokOther
)

// token is one lexed unit with its byte span in the input. open marks a
// string still waiting for its closing quote, and every comment, since typing
// at the end of either extends it.
type token struct {
	kind  int
	text  string // original text of an ident or a tokOther symbol
	upper string // uppercased ident text, for keyword checks
	start int
	end   int
	open  bool
}

// keywords are idents that can never be a bare source alias.
var keywords = map[string]bool{
	"AND": true, "ARRAY": true, "AS": true, "ASC": true, "BETWEEN": true, "BY": true,
	"CASE": true, "DESC": true, "DISTINCT": true, "ELSE": true, "END": true,
	"ESCAPE": true, "EXISTS": true, "FALSE": true, "FROM": true, "GROUP": true,
	"IN": true, "INNER": true, "IS": true, "JOIN": true, "LIKE": true, "LIMIT": true, "NOT": true,
	"NULL": true, "OFFSET": true, "ON": true, "OR": true, "ORDER": true,
	"SELECT": true, "THEN": true, "TOP": true, "TRUE": true, "UDF": true,
	"UNDEFINED": true, "VALUE": true, "WHEN": true, "WHERE": true,
}

// joinModifiers are the idents that may precede JOIN, and so can never be a
// bare source alias either.
var joinModifiers = map[string]bool{
	"CROSS": true, "FULL": true, "INNER": true, "LEFT": true, "OUTER": true, "RIGHT": true,
}

// source is one FROM-clause source: a dotted path plus an optional alias.
type source struct {
	path     []token
	alias    string
	start    int // byte offset of the first path token
	end      int // byte offset just past the last consumed token
	firstTok int
	nextTok  int    // index just past the last consumed token
	clause   int    // 1 for the first FROM clause of the query
	depth    int    // parentheses open around that FROM clause
	joined   bool   // introduced by JOIN rather than listed after FROM
	modifier string // what preceded that JOIN, e.g. "LEFT OUTER"
}

// lex splits s into tokens, treating single- and double-quoted regions as
// opaque string literals (backslash escapes respected) and `--` to the end
// of the line as a comment.
func lex(s string) []token {
	var toks []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '\'' || c == '"':
			end, closed := lexString(s, i)
			toks = append(toks, token{kind: tokString, start: i, end: end, open: !closed})
			i = end
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			end := lexComment(s, i)
			toks = append(toks, token{kind: tokComment, start: i, end: end, open: true})
			i = end
		case c >= utf8.RuneSelf && isSpaceRune(s[i:]):
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		case isIdentStart(c) || identRuneSize(s[i:]) > 0:
			start := i
			i = lexIdent(s, i)
			t := s[start:i]
			toks = append(toks, token{kind: tokIdent, text: t, upper: strings.ToUpper(t), start: start, end: i})
		case c >= utf8.RuneSelf:
			_, size := utf8.DecodeRuneInString(s[i:])
			toks = append(toks, token{kind: tokOther, text: s[i : i+size], start: i, end: i + size})
			i += size
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
			toks = append(toks, token{kind: tokOther, text: s[i : i+1], start: i, end: i + 1})
			i++
		}
	}
	return toks
}

// lexIdent returns the byte offset just past the identifier starting at i.
// A non-ASCII letter, digit or mark continues it as an ASCII one does.
func lexIdent(s string, i int) int {
	for i < len(s) {
		switch {
		case isIdentPart(s[i]):
			i++
		case identRuneSize(s[i:]) > 0:
			i += identRuneSize(s[i:])
		default:
			return i
		}
	}
	return i
}

// EndsName reports whether text ends with a dot or with a name as lex reads
// one; a number is no name.
func EndsName(text string) bool {
	if strings.HasSuffix(text, ".") {
		return true
	}
	start := len(text)
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if r < utf8.RuneSelf && !isIdentPart(byte(r)) || r >= utf8.RuneSelf && identRuneSize(text[start-size:]) == 0 {
			break
		}
		start -= size
	}
	return start < len(text) && !isDigit(text[start])
}

// identRuneSize is the byte length of the non-ASCII letter, digit or mark
// s starts with, or 0.
func identRuneSize(s string) int {
	r, size := utf8.DecodeRuneInString(s)
	if r < utf8.RuneSelf || !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
		return 0
	}
	return size
}

func isSpaceRune(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

// lexString returns the byte offset just past the string literal opening at
// i and whether it was closed; an unterminated literal consumes the rest of
// the input.
func lexString(s string, i int) (int, bool) {
	quote := s[i]
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
		case quote:
			return i + 1, true
		default:
			i++
		}
	}
	return len(s), false
}

// lexComment returns the byte offset of the line break ending the comment
// opening at i, or the end of the input. The service ends a comment at a
// lone \r as well as at \n.
func lexComment(s string, i int) int {
	if end := strings.IndexAny(s[i:], "\r\n"); end >= 0 {
		return i + end
	}
	return len(s)
}

// code drops the comments, which take no part in a query's structure. Text
// without one, the common case, keeps its tokens rather than a copy of them.
func code(toks []token) []token {
	if !slices.ContainsFunc(toks, func(tok token) bool { return tok.kind == tokComment }) {
		return toks
	}
	kept := make([]token, 0, len(toks))
	for _, tok := range toks {
		if tok.kind != tokComment {
			kept = append(kept, tok)
		}
	}
	return kept
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
	// elements are the aliases a `JOIN alias IN path` declared, in order.
	elements []element
	// declarations index the tokens that declare an alias; roots index
	// the first token of every source path, which names a database or a
	// container rather than reading an alias.
	declarations []int
	roots        []int
	// subqueryAliases name the (subquery) sources declared so far: a path
	// rooted at one reads the subquery's items, and is no container.
	subqueryAliases map[string]bool
	clauses         int
	depth           int
}

// element is an alias ranging over an array: the alias the array was
// written against and the property path under it.
type element struct {
	name string
	base string
	path []string
}

func parse(text string) *parser {
	return parseTokens(code(lex(text)))
}

func parseTokens(toks []token) *parser {
	p := &parser{toks: toks, aliases: map[string]bool{}, subqueryAliases: map[string]bool{}}
	p.run()
	return p
}

func (p *parser) run() {
	p.walk(0, len(p.toks))
}

// walk parses the FROM clauses among the tokens from index from up to to,
// tracking the parentheses around them.
func (p *parser) walk(from, to int) {
	for i := from; i < to; {
		switch {
		case p.isKeyword(i, "FROM"):
			i = p.parseFromClause(i + 1)
			continue
		case isSymbol(p.toks[i], "("):
			p.depth++
		case isSymbol(p.toks[i], ")"):
			p.depth--
		}
		i++
	}
}

func (p *parser) parseFromClause(i int) int {
	p.clauses++
	for {
		j, ok := p.parseFromSource(i)
		if !ok {
			return j
		}
		i = p.parseJoins(j)
		if !p.isComma(i) {
			return i
		}
		i++
	}
}

// parseFromSource consumes one source of a FROM list: a path with its alias
// and any IN collection, or a subquery with its alias.
func (p *parser) parseFromSource(i int) (int, bool) {
	src, j, ok := p.parseSource(i)
	if !ok {
		return p.parseSubquerySource(i)
	}
	if p.readsSubquery(src) {
		p.declareAliasOf(src)
		return j, true
	}
	p.record(src)
	if p.isKeyword(j, "IN") {
		j = p.skipCollection(j + 1)
	}
	return j, true
}

func (p *parser) parseJoins(i int) int {
	for {
		modifier, j := p.parseJoinModifier(i)
		if !p.isKeyword(j, "JOIN") {
			return i
		}
		i = p.parseJoinClause(j+1, modifier)
	}
}

func (p *parser) parseJoinModifier(i int) (string, int) {
	var words []string
	for i < len(p.toks) && p.toks[i].kind == tokIdent && joinModifiers[p.toks[i].upper] {
		words = append(words, p.toks[i].upper)
		i++
	}
	return strings.Join(words, " "), i
}

// parseJoinClause binds the alias of `JOIN alias IN collection` and skips its
// path; a bare `JOIN path` is recorded as a source so cross-container
// references are detected.
func (p *parser) parseJoinClause(i int, modifier string) int {
	src, j, ok := p.parseSource(i)
	if !ok {
		next, _ := p.parseSubquerySource(i)
		return next
	}
	if !p.isKeyword(j, "IN") && p.readsSubquery(src) {
		p.declareAliasOf(src)
		return j
	}
	if !p.isKeyword(j, "IN") {
		src.joined, src.modifier = true, modifier
		p.record(src)
		if p.isKeyword(j, "ON") {
			return p.skipOn(j + 1)
		}
		return j
	}
	collection, k, ok := p.parseSource(j + 1)
	if len(src.path) == 1 && src.alias == "" {
		p.aliases[src.path[0].text] = true
		p.declarations = append(p.declarations, src.firstTok)
		if ok && len(collection.path) > 1 {
			p.elements = append(p.elements, element{
				name: src.path[0].text,
				base: collection.path[0].text,
				path: pathText(collection.path[1:]),
			})
		}
	}
	if ok {
		return k
	}
	return j + 1
}

// parseSubquerySource consumes `(subquery) [AS] alias` at i and reports
// whether one is there. The subquery's own FROM clauses are parsed as any
// nested one is; the subquery is no source of the statement around it, and
// its alias is a declaration, not an alias the planner reads.
func (p *parser) parseSubquerySource(i int) (int, bool) {
	if !isSymbolAt(p.toks, i, "(") {
		return i, false
	}
	closing := matchingParen(p.toks, i)
	depth, clauses := p.depth, p.clauses
	p.walk(i, closing+1)
	p.depth, p.clauses = depth, clauses
	next := closing + 1
	if p.isKeyword(next, "AS") {
		next++
	}
	if p.isBareAlias(next) {
		p.declarations = append(p.declarations, next)
		p.subqueryAliases[p.toks[next].text] = true
		next++
	}
	return next, true
}

// readsSubquery reports whether src is a path under a subquery declared
// before it, as in JOIN x.items after (subquery) x.
func (p *parser) readsSubquery(src source) bool {
	return len(src.path) > 1 && p.subqueryAliases[src.path[0].text]
}

// declareAliasOf declares the alias of src, a path under a subquery, as a
// subquery's own alias is declared: a path under it reads the subquery's
// items too.
func (p *parser) declareAliasOf(src source) {
	if src.alias != "" {
		p.declarations = append(p.declarations, src.nextTok-1)
		p.subqueryAliases[src.alias] = true
	}
}

// skipCollection passes over the path of `FROM alias IN path`, whose root
// names the container.
func (p *parser) skipCollection(i int) int {
	collection, j, ok := p.parseSource(i)
	if !ok {
		return i
	}
	p.roots = append(p.roots, collection.firstTok)
	return j
}

// skipOn passes over an ON condition to whatever may follow it: another
// join, a comma, or a clause. It stops at a parenthesis too, leaving run to
// track the depth of anything nested.
func (p *parser) skipOn(i int) int {
	for ; i < len(p.toks); i++ {
		tok := p.toks[i]
		switch {
		case followsDot(p.toks, i):
		case isSymbol(tok, "("), isSymbol(tok, ")"), tok.kind == tokComma:
			return i
		case tok.kind == tokIdent && (clauseWords[tok.upper] || joinModifiers[tok.upper] || tok.upper == "JOIN"):
			return i
		}
	}
	return i
}

func pathText(path []token) []string {
	names := make([]string, 0, len(path))
	for _, tok := range path {
		names = append(names, tok.text)
	}
	return names
}

// parseSource consumes one dotted path plus an optional `AS alias` or bare
// alias.
func (p *parser) parseSource(i int) (source, int, bool) {
	if i >= len(p.toks) || p.toks[i].kind != tokIdent || keywords[p.toks[i].upper] {
		return source{}, i, false
	}
	src := source{path: []token{p.toks[i]}, start: p.toks[i].start, end: p.toks[i].end, firstTok: i}
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
	case p.isBareAlias(i):
		src.alias = p.toks[i].text
		src.end = p.toks[i].end
		i++
	}
	src.nextTok = i
	return src, i, true
}

func (p *parser) isBareAlias(i int) bool {
	if i >= len(p.toks) || p.toks[i].kind != tokIdent {
		return false
	}
	return !keywords[p.toks[i].upper] && !joinModifiers[p.toks[i].upper]
}

func (p *parser) record(src source) {
	src.clause, src.depth = p.clauses, p.depth
	p.sources = append(p.sources, src)
	p.roots = append(p.roots, src.firstTok)
	switch {
	case src.alias != "":
		p.aliases[src.alias] = true
		p.declarations = append(p.declarations, src.nextTok-1)
	case len(src.path) == 1:
		p.declarations = append(p.declarations, src.firstTok)
	}
}

func (p *parser) isKeyword(i int, kw string) bool {
	return keywordAt(p.toks, i, kw)
}

func (p *parser) isComma(i int) bool {
	return i < len(p.toks) && p.toks[i].kind == tokComma
}
