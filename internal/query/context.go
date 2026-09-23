package query

import (
	"slices"
	"unicode/utf8"
)

// CompletionKind is what belongs at the cursor.
type CompletionKind int

const (
	CompleteNothing    CompletionKind = iota
	CompleteKeyword                   // Keywords only
	CompleteSource                    // databases, and the catalog's scope container as a bare alias
	CompleteDatabase                  // databases only
	CompleteContainer                 // the containers of Database
	CompleteField                     // the fields under the one Alias, at Path
	CompleteReference                 // every Alias, then its fields
	CompleteExpression                // CompleteReference plus functions and Keywords
)

// Completion is what can be typed at a cursor: the word under it, the byte
// range a suggestion replaces, and what kind of thing belongs there.
type Completion struct {
	Kind CompletionKind
	// Word is the part of the token under the cursor typed so far; Start and
	// End cover the whole token.
	Word  string
	Start int
	End   int
	// Database names the one whose containers complete.
	Database string
	// Aliases is the alias under the cursor for a field, and every alias the
	// query declares, in order, for a reference or an expression.
	Aliases []Alias
	// Path is the property path typed after a field's alias.
	Path []string
	// Keywords are the words valid here, in the order they are offered.
	Keywords []string
	// TopLevel marks a field position that only top-level fields fit: the
	// SELECT list of a cross-container join.
	TopLevel bool
}

// Alias is a name a query reads items through.
type Alias struct {
	Name string
	// Scopes are the db.container paths the alias is bound to: one for a
	// single container or a join side, several for a container list, and
	// none when the query names no container and the catalog's scope applies.
	Scopes [][]string
	// Path is where under those containers the alias's items sit: empty at
	// the root, and the array's element path ("tags[]") for a `JOIN alias
	// IN` alias.
	Path []string
}

// WithDefaultScope binds every alias the query left unbound to scope.
func (c Completion) WithDefaultScope(scope []string) Completion {
	if len(scope) == 0 {
		return c
	}
	c.Aliases = slices.Clone(c.Aliases)
	for i := range c.Aliases {
		if len(c.Aliases[i].Scopes) == 0 {
			c.Aliases[i].Scopes = [][]string{scope}
		}
	}
	return c
}

// Keyword groups, in the order they are offered.
var (
	literalKeywords    = []string{"NOT", "EXISTS", "ARRAY", "TRUE", "FALSE", "NULL", "UNDEFINED"}
	operatorKeywords   = []string{"AND", "OR", "NOT", "IN", "LIKE", "BETWEEN"}
	selectKeywords     = []string{"DISTINCT", "TOP", "VALUE"}
	negatedOperators   = []string{"IN", "LIKE", "BETWEEN"}
	clauseWords        = map[string]bool{"SELECT": true, "FROM": true, "WHERE": true, "GROUP": true, "ORDER": true, "OFFSET": true, "LIMIT": true}
	sourceBreakers     = map[string]bool{"JOIN": true, "ON": true, "IN": true}
	valueEndingSymbols = map[string]bool{")": true, "*": true, "]": true}
)

// Context reports what can be typed at cursor, a byte offset into text. The
// range it names lies inside text, on rune boundaries, for any cursor.
func Context(text string, cursor int) Completion {
	cursor = snapToRune(text, cursor)
	toks := lex(text)
	if insideLiteral(toks, cursor) {
		return Completion{Start: cursor, End: cursor}
	}
	toks = code(toks)
	word, before := splitAtCursor(toks, cursor)
	c := classifier{toks: before, parser: parseTokens(toks), typing: word.end > word.start}
	completion := c.classify()
	completion.Word = text[word.start:cursor]
	completion.Start, completion.End = word.start, word.end
	return completion
}

func snapToRune(text string, cursor int) int {
	cursor = min(max(cursor, 0), len(text))
	for cursor > 0 && cursor < len(text) && !utf8.RuneStart(text[cursor]) {
		cursor--
	}
	return cursor
}

// insideLiteral reports whether typing at cursor extends a string or a
// comment.
func insideLiteral(toks []token, cursor int) bool {
	for _, tok := range toks {
		if tok.kind != tokString && tok.kind != tokComment {
			continue
		}
		if tok.start < cursor && (cursor < tok.end || tok.open && cursor == tok.end) {
			return true
		}
	}
	return false
}

// splitAtCursor finds the identifier the cursor touches — an empty range at
// the cursor when there is none — and the tokens before it.
func splitAtCursor(toks []token, cursor int) (word token, before []token) {
	for i, tok := range toks {
		if tok.kind == tokIdent && tok.start <= cursor && cursor <= tok.end {
			return tok, toks[:i]
		}
		if tok.end > cursor {
			return token{start: cursor, end: cursor}, toks[:i]
		}
	}
	return token{start: cursor, end: cursor}, toks
}

// classifier reads the tokens before the cursor against the parse of the
// whole query. typing marks a word under the cursor: where that word may be
// a name the user is inventing, an alias, nothing is offered, since
// accepting a suggestion would overwrite it.
type classifier struct {
	toks   []token
	parser *parser
	typing bool
}

func (c classifier) classify() Completion {
	last, ok := c.last()
	if !ok {
		return Completion{Kind: CompleteKeyword, Keywords: []string{"SELECT"}}
	}
	if last.kind == tokDot {
		return c.afterDot()
	}
	clause, since := c.clause()
	switch clause {
	case "":
		return Completion{Kind: CompleteKeyword, Keywords: []string{"SELECT"}}
	case "SELECT":
		return c.inSelect(since)
	case "FROM":
		return c.inFrom(since)
	case "WHERE":
		return c.inWhere(since)
	case "GROUP", "ORDER":
		return c.inOrdering(clause, since)
	case "OFFSET":
		if len(since) == 0 {
			return Completion{}
		}
		return keywordsOnly([]string{"LIMIT"})
	}
	return Completion{}
}

func (c classifier) last() (token, bool) {
	if len(c.toks) == 0 {
		return token{}, false
	}
	return c.toks[len(c.toks)-1], true
}

// clause is the clause the cursor is in and the tokens since its keyword.
// Parentheses open a frame of their own, which inherits the clause around
// it until a SELECT of its own replaces it.
func (c classifier) clause() (string, []token) {
	type frame struct {
		clause string
		start  int
	}
	stack := []frame{{}}
	for i, tok := range c.toks {
		top := &stack[len(stack)-1]
		switch {
		case isSymbol(tok, "("):
			stack = append(stack, frame{clause: top.clause, start: i + 1})
		case isSymbol(tok, ")"):
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case tok.kind == tokIdent && clauseWords[tok.upper] && !followsDot(c.toks, i):
			top.clause, top.start = tok.upper, i+1
		}
	}
	top := stack[len(stack)-1]
	return top.clause, c.toks[top.start:]
}

func (c classifier) inSelect(since []token) Completion {
	last, ok := lastOf(since)
	switch {
	case !ok:
		return c.expression(append(slices.Clone(selectKeywords), literalKeywords...))
	case last.upper == "TOP", last.upper == "AS":
		return Completion{}
	case last.kind == tokNumber && len(since) > 1 && since[len(since)-2].upper == "TOP":
		return c.expression(literalKeywords)
	case endsValue(last) && c.typing && !isSymbol(last, "*"):
		return Completion{}
	case endsValue(last):
		return keywordsOnly([]string{"FROM", "AS"})
	}
	return c.expression(literalKeywords)
}

// inFrom reads the FROM clause from its last structural token: the keyword
// that opened the current source, the comma that listed it, or the ON or IN
// that followed it.
func (c classifier) inFrom(since []token) Completion {
	if last, ok := lastOf(since); ok && last.upper == "INNER" {
		return c.afterInner()
	}
	breaker, tail := splitFrom(since)
	switch breaker {
	case "JOIN":
		return c.afterJoin(tail)
	case "ON":
		return c.inOn(tail)
	case "IN":
		if len(tail) == 0 {
			return c.reference()
		}
		return keywordsOnly(c.afterSources())
	case ",":
		return c.afterSource(tail, CompleteDatabase)
	}
	return c.afterSource(tail, CompleteSource)
}

// splitFrom names the last structural token of a FROM clause and returns the
// tokens after it. The empty name is the FROM keyword itself.
func splitFrom(since []token) (string, []token) {
	name, start, depth := "", 0, 0
	for i, tok := range since {
		switch {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		case depth == 0 && tok.kind == tokComma:
			name, start = ",", i+1
		case depth == 0 && tok.kind == tokIdent && sourceBreakers[tok.upper] && !followsDot(since, i):
			name, start = tok.upper, i+1
		}
	}
	return name, since[start:]
}

// afterSource completes what follows a listed source: its alias is the
// user's to invent, so only the keywords that may come next are offered.
func (c classifier) afterSource(tail []token, empty CompletionKind) Completion {
	last, ok := lastOf(tail)
	switch {
	case !ok:
		return Completion{Kind: empty}
	case last.upper == "AS":
		return Completion{}
	case last.kind == tokIdent && joinModifiers[last.upper]:
		return Completion{}
	case isPath(tail) && c.typing:
		return Completion{}
	case isPath(tail):
		return keywordsOnly(append([]string{"AS"}, c.afterSources()...))
	}
	return keywordsOnly(c.afterSources())
}

// afterInner offers the JOIN the modifier opens, unless the query already
// holds the one cross-container join the planner runs.
func (c classifier) afterInner() Completion {
	if c.crossJoin() {
		return Completion{}
	}
	return keywordsOnly([]string{"JOIN"})
}

func (c classifier) afterJoin(tail []token) Completion {
	last, ok := lastOf(tail)
	switch {
	case !ok && c.crossJoin():
		return Completion{}
	case !ok:
		return Completion{Kind: CompleteDatabase}
	case last.upper == "AS", isPath(tail) && c.typing:
		return Completion{}
	case isPath(tail) && len(tail) == 1:
		return keywordsOnly([]string{"IN"})
	case isPath(tail):
		return keywordsOnly([]string{"AS", "ON"})
	}
	return keywordsOnly([]string{"ON"})
}

func (c classifier) inOn(tail []token) Completion {
	last, ok := lastOf(tail)
	switch {
	case !ok, isSymbol(last, "="):
		return c.reference()
	case endsValue(last) && slices.ContainsFunc(tail, func(t token) bool { return isSymbol(t, "=") }):
		return keywordsOnly(c.afterSources())
	}
	return Completion{}
}

func (c classifier) inWhere(since []token) Completion {
	last, ok := lastOf(since)
	switch {
	case !ok:
		return c.expression(literalKeywords)
	case last.upper == "NOT" && len(since) > 1 && endsValue(since[len(since)-2]):
		return keywordsOnly(negatedOperators)
	case endsValue(last) && len(since) > 1 && since[len(since)-2].upper == "LIKE":
		return keywordsOnly(slices.Concat([]string{"ESCAPE"}, operatorKeywords, c.following("WHERE")))
	case endsValue(last):
		return keywordsOnly(slices.Concat(operatorKeywords, c.following("WHERE")))
	}
	return c.expression(literalKeywords)
}

func (c classifier) inOrdering(clause string, since []token) Completion {
	if len(since) == 0 {
		return keywordsOnly([]string{"BY"})
	}
	if since[0].upper == "BY" {
		since = since[1:]
	}
	last, ok := lastOf(since)
	switch {
	case !ok, last.kind == tokComma:
		return c.expression(nil)
	case clause == "ORDER" && endsValue(last):
		return keywordsOnly(append([]string{"ASC", "DESC"}, c.following(clause)...))
	case endsValue(last), last.upper == "ASC", last.upper == "DESC":
		return keywordsOnly(c.following(clause))
	}
	return c.expression(nil)
}

// afterDot completes a dotted name: the containers of a database in a source
// position, and the fields under an alias anywhere else.
func (c classifier) afterDot() Completion {
	chain, start := c.dottedChain()
	if len(chain) == 0 {
		return Completion{}
	}
	if c.sourcePosition(start) {
		if len(chain) > 1 {
			return Completion{}
		}
		return Completion{Kind: CompleteContainer, Database: chain[0]}
	}
	clause, _ := c.clause()
	completion := Completion{
		Kind:     CompleteField,
		Aliases:  []Alias{c.resolve(chain[0])},
		TopLevel: clause == "SELECT" && c.crossJoin(),
	}
	if len(chain) > 1 {
		completion.Path = chain[1:]
	}
	return completion
}

// dottedChain reads the identifiers before the trailing dot and reports the
// index of the first.
func (c classifier) dottedChain() ([]string, int) {
	i := len(c.toks) - 1
	var chain []string
	for i > 0 && c.toks[i].kind == tokDot && c.toks[i-1].kind == tokIdent {
		chain = append([]string{c.toks[i-1].text}, chain...)
		i -= 2
	}
	return chain, i + 1
}

// sourcePosition reports whether a source may start at index i: right after
// FROM, JOIN, or a comma of a FROM list.
func (c classifier) sourcePosition(i int) bool {
	clause, since := c.clause()
	if clause != "FROM" {
		return false
	}
	breaker, tail := splitFrom(since)
	if breaker == "ON" || breaker == "IN" {
		return false
	}
	return len(tail) > 0 && tail[0].start == c.toks[i].start
}

// afterSources are the words that may follow a complete FROM clause. A join
// is offered only where the planner would run it: not after a container
// list, and not alongside a cross-container join already written.
func (c classifier) afterSources() []string {
	words := []string{"WHERE"}
	if !c.crossJoin() && !c.containerList() {
		words = append(words, "JOIN", "INNER")
	}
	return append(words, c.following("FROM")...)
}

// following names the clauses that may open after clause. A cross-container
// join is merged client-side, so nothing the merge cannot honor is offered
// after it.
func (c classifier) following(clause string) []string {
	if c.crossJoin() {
		return nil
	}
	switch clause {
	case "FROM", "WHERE":
		return []string{"GROUP BY", "ORDER BY", "OFFSET"}
	case "GROUP":
		return []string{"ORDER BY", "OFFSET"}
	case "ORDER":
		return []string{"OFFSET"}
	}
	return nil
}

func (c classifier) expression(keywords []string) Completion {
	last, ok := c.last()
	if ok && isSymbol(last, "(") {
		keywords = append([]string{"SELECT"}, keywords...)
	}
	return Completion{Kind: CompleteExpression, Aliases: c.aliases(), Keywords: keywords}
}

func (c classifier) reference() Completion {
	return Completion{Kind: CompleteReference, Aliases: c.aliases()}
}

func (c classifier) crossJoin() bool {
	containers := c.parser.containerSources()
	return len(containers) > 1 && containers[len(containers)-1].joined
}

func (c classifier) containerList() bool {
	containers := c.parser.containerSources()
	return len(containers) > 1 && !containers[len(containers)-1].joined
}

// aliases lists the query's aliases as BuildPlan reads them, root aliases
// first, then the ones a `JOIN alias IN` declares.
func (c classifier) aliases() []Alias {
	aliases := c.rootAliases()
	for _, element := range c.parser.elements {
		aliases = append(aliases, elementAlias(aliases, element))
	}
	return aliases
}

func (c classifier) rootAliases() []Alias {
	containers := c.parser.containerSources()
	switch {
	case len(containers) == 0:
		return c.unboundAlias()
	case len(containers) == 1:
		return []Alias{{Name: aliasOrDefault(containers[0].alias), Scopes: [][]string{scopeOf(containers[0])}}}
	case containers[len(containers)-1].joined:
		sides := make([]Alias, 0, len(containers))
		for _, side := range containers {
			sides = append(sides, Alias{Name: joinAlias(side), Scopes: [][]string{scopeOf(side)}})
		}
		return sides
	}
	alias, err := sharedAlias(containers)
	if err != nil {
		alias = aliasOrDefault(containers[0].alias)
	}
	scopes := make([][]string, 0, len(containers))
	for _, container := range containers {
		scopes = append(scopes, scopeOf(container))
	}
	return []Alias{{Name: alias, Scopes: scopes}}
}

// unboundAlias is the name a query with no container reads through, left
// for the catalog's scope to bind.
func (c classifier) unboundAlias() []Alias {
	for _, s := range c.parser.sources {
		if s.clause != 1 || s.depth != 0 || s.joined {
			continue
		}
		name := s.alias
		if name == "" {
			name = s.path[0].text
		}
		return []Alias{{Name: name}}
	}
	return nil
}

// elementAlias binds a `JOIN alias IN base.path` alias to the array's
// elements under the containers of base.
func elementAlias(known []Alias, e element) Alias {
	alias := Alias{Name: e.name}
	i := slices.IndexFunc(known, func(a Alias) bool { return a.Name == e.base })
	if i >= 0 {
		alias.Scopes = known[i].Scopes
		alias.Path = slices.Clone(known[i].Path)
	}
	alias.Path = append(alias.Path, e.path[:len(e.path)-1]...)
	alias.Path = append(alias.Path, e.path[len(e.path)-1]+"[]")
	return alias
}

func (c classifier) resolve(name string) Alias {
	for _, alias := range c.aliases() {
		if alias.Name == name {
			return alias
		}
	}
	return Alias{Name: name}
}

func keywordsOnly(words []string) Completion {
	return Completion{Kind: CompleteKeyword, Keywords: words}
}

func lastOf(toks []token) (token, bool) {
	if len(toks) == 0 {
		return token{}, false
	}
	return toks[len(toks)-1], true
}

// endsValue reports whether tok closes an operand, so an operator or a
// clause keyword may follow.
func endsValue(tok token) bool {
	switch tok.kind {
	case tokIdent:
		return !keywords[tok.upper]
	case tokNumber, tokString:
		return true
	}
	return tok.kind == tokOther && valueEndingSymbols[tok.text]
}

// isPath reports whether toks are one dotted name and nothing else.
func isPath(toks []token) bool {
	if len(toks) == 0 || len(toks)%2 == 0 {
		return false
	}
	for i, tok := range toks {
		if i%2 == 0 && tok.kind != tokIdent || i%2 == 1 && tok.kind != tokDot {
			return false
		}
	}
	return true
}
