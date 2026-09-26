package query

import (
	"maps"
	"slices"
)

// Shapes the planner refuses, named in its errors.
const (
	shapeOn           = "an ON clause other than one equality between a field of the joined container and a field of an earlier one"
	shapeProjection   = "a join SELECT list other than * or alias.field [AS name] items"
	shapePropertyJoin = "JOIN ... IN alongside a cross-container join: use CROSS APPLY"
	shapeListWithJoin = "a container list mixed with a join"
	shapeApplyQuery   = "APPLY of a query over another container: it would run one query per row"
	hintCTE           = ": put it in a CTE, where the service evaluates it"
)

var (
	// trailingClauses may follow a WHERE clause, or stand in for one.
	trailingClauses = map[string]bool{"ORDER": true, "GROUP": true, "OFFSET": true, "LIMIT": true}
	// whereBreakers are the keywords that decide how a join's WHERE clause
	// can be divided between its sides.
	whereBreakers = withKeys(trailingClauses, "AND", "OR", "BETWEEN")
)

func withKeys(set map[string]bool, keys ...string) map[string]bool {
	extended := maps.Clone(set)
	for _, key := range keys {
		extended[key] = true
	}
	return extended
}

// fragment is a run of tokens of the editor text, with the byte range it
// spans in that text.
type fragment struct {
	text       string
	toks       []token
	start, end int
}

// sub is the fragment of toks, a non-empty run of f's tokens.
func (f fragment) sub(toks []token) fragment {
	return fragment{text: f.text, toks: toks, start: toks[0].start, end: toks[len(toks)-1].end}
}

// statement is an editor query read as `[WITH cte, ...] body`.
type statement struct {
	ctes []cteText
	main fragment
}

type cteText struct {
	name string
	body fragment
}

// names are the CTEs the statement declares, in order.
func (s statement) names() []string {
	names := make([]string, 0, len(s.ctes))
	for _, c := range s.ctes {
		names = append(names, c.name)
	}
	return names
}

func parseStatement(text string) (statement, error) {
	toks := code(lex(text))
	whole := fragment{text: text, toks: toks, end: len(text)}
	if !keywordAt(toks, 0, "WITH") {
		return statement{main: whole}, nil
	}
	if keywordAt(toks, 1, "RECURSIVE") {
		return statement{}, unsupported("a recursive CTE")
	}
	var stmt statement
	i := 1
	for {
		cte, next, err := parseCTE(whole, i)
		if err != nil {
			return statement{}, err
		}
		stmt.ctes = append(stmt.ctes, cte)
		i = next
		if i >= len(toks) || toks[i].kind != tokComma {
			break
		}
		i++
	}
	if i >= len(toks) {
		return statement{}, unsupported("a WITH clause with no query after it")
	}
	stmt.main = fragment{text: text, toks: toks[i:], start: toks[i].start, end: len(text)}
	return stmt, nil
}

// parseCTE reads `name AS ( body )` at i and returns the index past it.
func parseCTE(whole fragment, i int) (cteText, int, error) {
	toks := whole.toks
	if i >= len(toks) || toks[i].kind != tokIdent || !keywordAt(toks, i+1, "AS") || !isSymbolAt(toks, i+2, "(") {
		return cteText{}, i, unsupported("a WITH clause other than name AS (query), ...")
	}
	name := toks[i]
	if keywords[name.upper] || joinModifiers[name.upper] {
		return cteText{}, i, unsupported("a CTE named " + name.text + ", which is a keyword")
	}
	closing := matchingParen(toks, i+2)
	if closing < 0 {
		return cteText{}, i, unsupported("a CTE whose parenthesis is never closed")
	}
	body := toks[i+3 : closing]
	if len(body) == 0 {
		return cteText{}, i, unsupported("an empty CTE " + name.text)
	}
	return cteText{name: name.text, body: whole.sub(body)}, closing + 1, nil
}

// matchingParen is the index of the parenthesis closing the one at open, or
// -1 when there is none.
func matchingParen(toks []token, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch {
		case isSymbol(toks[i], "("):
			depth++
		case isSymbol(toks[i], ")"):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// body is a simulated query read as
// `SELECT list FROM input (join input)* [WHERE conjunct (AND conjunct)*]`.
type body struct {
	projection []token
	inputs     []inputText
	steps      []stepText // steps[i] attaches inputs[i+1]
	where      []token
	hasWhere   bool
}

type inputText struct {
	path    []token
	alias   string // as written, empty when none was
	applies []applyText
}

// name is what the input is known by: its alias, else the last part of its
// path.
func (in inputText) name() string {
	if in.alias != "" {
		return in.alias
	}
	return in.path[len(in.path)-1].text
}

type applyText struct {
	outer bool
	alias string
	array []token // alias.path
}

type stepText struct {
	kind JoinKind
	on   []token
}

// descent reads a body by recursive descent. Every method takes the index to
// read from and returns the index just past what it consumed.
type descent struct {
	toks []token
}

func parseBody(f fragment) (body, error) {
	d := descent{toks: f.toks}
	from := d.topLevel(0, "FROM")
	if !keywordAt(d.toks, 0, "SELECT") || from < 0 {
		return body{}, unsupported(shapeProjection)
	}
	b := body{projection: d.toks[1:from]}
	first, i, err := d.parseInput(from + 1)
	if err != nil {
		return body{}, err
	}
	b.inputs = append(b.inputs, first)
	for {
		kind, next, isJoin := d.parseJoinKeywords(i)
		if !isJoin {
			break
		}
		input, step, after, err := d.parseJoin(next, kind)
		if err != nil {
			return body{}, err
		}
		b.inputs = append(b.inputs, input)
		b.steps = append(b.steps, step)
		i = after
	}
	return d.parseTail(b, i)
}

// topLevel is the index of the first keyword outside parentheses at or after
// i, or -1.
func (d descent) topLevel(i int, keyword string) int {
	depth := 0
	for ; i < len(d.toks); i++ {
		switch {
		case isSymbol(d.toks[i], "("):
			depth++
		case isSymbol(d.toks[i], ")"):
			depth--
		case depth == 0 && keywordAt(d.toks, i, keyword) && !followsDot(d.toks, i):
			return i
		}
	}
	return -1
}

func (d descent) parseTail(b body, i int) (body, error) {
	switch {
	case i == len(d.toks):
		return b, nil
	case d.toks[i].kind == tokComma:
		return body{}, unsupported(shapeListWithJoin)
	case keywordAt(d.toks, i, "WHERE"):
		b.where, b.hasWhere = d.toks[i+1:], true
		return b, nil
	case d.toks[i].kind == tokIdent && trailingClauses[d.toks[i].upper]:
		return body{}, trailingClause(d.toks[i].upper)
	}
	return body{}, unsupported(shapeOn)
}

func trailingClause(keyword string) error {
	if keyword == "ORDER" {
		return unsupported("ORDER in a cross-container join" + hintCTE)
	}
	return unsupported(keyword + " in a cross-container join")
}

// parseInput reads a source and the APPLYs that follow it.
func (d descent) parseInput(i int) (inputText, int, error) {
	input, i, err := d.parseSource(i)
	if err != nil {
		return inputText{}, i, err
	}
	input.applies, i, err = d.parseApplies(i)
	return input, i, err
}

// parseSource reads `path [[AS] alias]`.
func (d descent) parseSource(i int) (inputText, int, error) {
	if i >= len(d.toks) || d.toks[i].kind != tokIdent || keywords[d.toks[i].upper] {
		return inputText{}, i, unsupported("a source other than a container or a CTE")
	}
	input := inputText{path: []token{d.toks[i]}}
	i++
	for i+1 < len(d.toks) && d.toks[i].kind == tokDot && d.toks[i+1].kind == tokIdent {
		input.path = append(input.path, d.toks[i+1])
		i += 2
	}
	switch {
	case keywordAt(d.toks, i, "AS") && i+1 < len(d.toks) && d.toks[i+1].kind == tokIdent:
		input.alias = d.toks[i+1].text
		i += 2
	case d.isBareAlias(i):
		if syntax := misreadAlias(d.toks, i); syntax != "" {
			return inputText{}, i, unsupported(syntax)
		}
		input.alias = d.toks[i].text
		i++
	}
	if len(input.path) > 2 {
		return inputText{}, i, unsupported("a source other than a container or a CTE")
	}
	return input, i, nil
}

func (d descent) isBareAlias(i int) bool {
	return i < len(d.toks) && d.toks[i].kind == tokIdent && !keywords[d.toks[i].upper] && !joinModifiers[d.toks[i].upper]
}

// misreadAlias names the join syntax that the word at i, read as a bare
// alias, really starts.
func misreadAlias(toks []token, i int) string {
	switch {
	case toks[i].upper == "NATURAL" && keywordAt(toks, i+1, "JOIN"):
		return "NATURAL JOIN"
	case toks[i].upper == "USING":
		return "JOIN ... USING"
	}
	return ""
}

func (d descent) parseApplies(i int) ([]applyText, int, error) {
	var applies []applyText
	for (keywordAt(d.toks, i, "CROSS") || keywordAt(d.toks, i, "OUTER")) && keywordAt(d.toks, i+1, "APPLY") {
		apply, next, err := d.parseApply(i+2, d.toks[i].upper == "OUTER")
		if err != nil {
			return nil, next, err
		}
		applies = append(applies, apply)
		i = next
	}
	return applies, i, nil
}

// parseApply reads `alias IN alias.path` after APPLY.
func (d descent) parseApply(i int, outer bool) (applyText, int, error) {
	if applyOfQuery(d.toks, i) {
		return applyText{}, i, unsupported(shapeApplyQuery)
	}
	if i >= len(d.toks) || !d.isBareAlias(i) || !keywordAt(d.toks, i+1, "IN") {
		return applyText{}, i, unsupported("APPLY other than alias IN alias.path")
	}
	apply := applyText{outer: outer, alias: d.toks[i].text}
	end := pathEnd(d.toks, i+2)
	apply.array = d.toks[i+2 : end]
	if len(apply.array) < 3 {
		return applyText{}, end, unsupported("APPLY other than alias IN alias.path")
	}
	return apply, end, nil
}

// applyOfQuery reports whether the APPLY whose operand starts at i applies a
// subquery or a function rather than an array.
func applyOfQuery(toks []token, i int) bool {
	return isSymbolAt(toks, i, "(") || (i < len(toks) && toks[i].kind == tokIdent && isSymbolAt(toks, i+1, "("))
}

// pathEnd is the index past the dotted name starting at i.
func pathEnd(toks []token, i int) int {
	if i >= len(toks) || toks[i].kind != tokIdent {
		return i
	}
	i++
	for i+1 < len(toks) && toks[i].kind == tokDot && toks[i+1].kind == tokIdent {
		i += 2
	}
	return i
}

// parseJoinKeywords reads `[INNER | (LEFT | RIGHT | FULL) [OUTER] | CROSS] JOIN`.
func (d descent) parseJoinKeywords(i int) (JoinKind, int, bool) {
	kind := InnerJoin
	switch {
	case keywordAt(d.toks, i, "INNER"):
		i++
	case keywordAt(d.toks, i, "CROSS"):
		kind = CrossJoin
		i++
	case keywordAt(d.toks, i, "LEFT"), keywordAt(d.toks, i, "RIGHT"), keywordAt(d.toks, i, "FULL"):
		kind = outerKinds[d.toks[i].upper]
		i++
		if keywordAt(d.toks, i, "OUTER") {
			i++
		}
	}
	return kind, i + 1, keywordAt(d.toks, i, "JOIN")
}

var outerKinds = map[string]JoinKind{"LEFT": LeftOuterJoin, "RIGHT": RightOuterJoin, "FULL": FullOuterJoin}

// parseJoin reads what follows JOIN: a source, its ON clause, and the APPLYs
// after that.
func (d descent) parseJoin(i int, kind JoinKind) (inputText, stepText, int, error) {
	if keywordAt(d.toks, i+1, "IN") {
		return inputText{}, stepText{}, i, unsupported(shapePropertyJoin)
	}
	input, i, err := d.parseSource(i)
	if err != nil {
		return inputText{}, stepText{}, i, err
	}
	if keywordAt(d.toks, i, "USING") {
		return inputText{}, stepText{}, i, unsupported("JOIN ... USING")
	}
	step := stepText{kind: kind}
	if keywordAt(d.toks, i, "ON") {
		end := d.onEnd(i + 1)
		step.on = d.toks[i+1 : end]
		i = end
	}
	switch {
	case kind == CrossJoin && step.on != nil:
		return inputText{}, stepText{}, i, unsupported(shapeOn + ", and none on CROSS JOIN")
	case kind != CrossJoin && len(step.on) == 0:
		return inputText{}, stepText{}, i, unsupported(shapeOn)
	}
	input.applies, i, err = d.parseApplies(i)
	return input, step, i, err
}

// onEnd is the index past an ON condition: the next join, APPLY, clause or
// comma outside parentheses.
func (d descent) onEnd(i int) int {
	depth := 0
	for ; i < len(d.toks); i++ {
		tok := d.toks[i]
		switch {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		case depth > 0 || followsDot(d.toks, i):
		case tok.kind == tokComma:
			return i
		case tok.kind == tokIdent && (tok.upper == "JOIN" || tok.upper == "WHERE" || joinModifiers[tok.upper] || trailingClauses[tok.upper]):
			return i
		}
	}
	return i
}

// splitConjuncts cuts conditions at their top-level ANDs. An OR would bind
// looser than those ANDs and BETWEEN brings an AND of its own, so either one
// keeps the clause whole.
func splitConjuncts(conditions []token) ([][]token, error) {
	var cuts []int
	whole := false
	for _, i := range topLevelBreakers(conditions) {
		switch conditions[i].upper {
		case "AND":
			cuts = append(cuts, i)
		case "OR", "BETWEEN":
			whole = true
		default:
			return nil, trailingClause(conditions[i].upper)
		}
	}
	if whole {
		return [][]token{conditions}, nil
	}
	return cutAt(conditions, cuts), nil
}

// splitOn cuts an ON clause at its top-level ANDs. There is no keeping it
// whole: its first conjunct must be the equality.
func splitOn(on []token) ([][]token, error) {
	var cuts []int
	for _, i := range topLevelBreakers(on) {
		if on[i].upper != "AND" {
			return nil, unsupported(shapeOn + ", then AND conditions, with any BETWEEN in parentheses")
		}
		cuts = append(cuts, i)
	}
	return cutAt(on, cuts), nil
}

func cutAt(toks []token, cuts []int) [][]token {
	var parts [][]token
	start := 0
	for _, cut := range cuts {
		parts = append(parts, toks[start:cut])
		start = cut + 1
	}
	return append(parts, toks[start:])
}

// topLevelBreakers indexes the whereBreakers outside any parentheses; one
// that follows a dot names a property instead.
func topLevelBreakers(toks []token) []int {
	var breakers []int
	depth := 0
	for i, tok := range toks {
		switch {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		case depth == 0 && whereBreakers[tok.upper] && !followsDot(toks, i):
			breakers = append(breakers, i)
		}
	}
	return breakers
}

// absentTest is the alias of a conjunct that is exactly `NOT IS_DEFINED(alias)`.
func absentTest(conjunct []token) (string, bool) {
	if len(conjunct) != 5 || !keywordAt(conjunct, 0, "NOT") {
		return "", false
	}
	return definedTest(conjunct[1:])
}

// definedTest is the alias of a conjunct that is exactly `IS_DEFINED(alias)`.
func definedTest(conjunct []token) (string, bool) {
	ok := len(conjunct) == 4 && keywordAt(conjunct, 0, "IS_DEFINED") && isSymbol(conjunct[1], "(") &&
		conjunct[2].kind == tokIdent && isSymbol(conjunct[3], ")")
	if !ok {
		return "", false
	}
	return conjunct[2].text, true
}

// aliasesRead are the names among aliases that toks read, other than as a
// property after a dot.
func aliasesRead(toks []token, aliases []string) []string {
	var read []string
	for i, tok := range toks {
		if tok.kind == tokIdent && !followsDot(toks, i) && slices.Contains(aliases, tok.text) && !slices.Contains(read, tok.text) {
			read = append(read, tok.text)
		}
	}
	return read
}

// fieldRef reads `alias.field[.field...]` spanning all of toks.
func fieldRef(toks []token) (FieldRef, bool) {
	if len(toks) < 3 || toks[0].kind != tokIdent || pathEnd(toks, 0) != len(toks) {
		return FieldRef{}, false
	}
	ref := FieldRef{Alias: toks[0].text}
	for i := 2; i < len(toks); i += 2 {
		ref.Path = append(ref.Path, toks[i].text)
	}
	return ref, true
}

func isSymbol(tok token, symbol string) bool {
	return tok.kind == tokOther && tok.text == symbol
}

func followsDot(toks []token, i int) bool {
	return i > 0 && toks[i-1].kind == tokDot
}

func keywordAt(toks []token, i int, keyword string) bool {
	return i >= 0 && i < len(toks) && toks[i].kind == tokIdent && toks[i].upper == keyword
}
