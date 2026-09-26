package query

import (
	"maps"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Shapes of a cross-container join the planner refuses.
const (
	shapeOn           = "an ON clause other than one equality between a field of the joined container and a field of an earlier one"
	shapeProjection   = "a join SELECT list other than * or alias.field [AS name] items"
	shapePropertyJoin = "JOIN ... IN alongside a cross-container join"
	shapeListWithJoin = "a container list mixed with a join"
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

// joinPlanner reads `SELECT list FROM first (JOIN next ON a.x = b.y)+ [WHERE ...]`.
// A side is the index of its container in written order throughout.
type joinPlanner struct {
	text    string
	toks    []token
	aliases []string
}

func planJoin(text string, toks []token, containers []source) (Plan, error) {
	j := joinPlanner{text: text, toks: toks}
	for _, c := range containers {
		j.aliases = append(j.aliases, joinAlias(c))
	}
	if err := j.checkSources(containers); err != nil {
		return Plan{}, err
	}
	columns, err := j.parseProjection(containers[0].firstTok - 1)
	if err != nil {
		return Plan{}, err
	}
	steps, next, err := j.parseChain(containers)
	if err != nil {
		return Plan{}, err
	}
	filters, err := j.parseWhere(next)
	if err != nil {
		return Plan{}, err
	}
	leaves := make([]Leaf, 0, len(containers))
	for side, c := range containers {
		leaves = append(leaves, j.leaf(side, c, filters[side]))
	}
	return Plan{Merge: HashJoin, Leaves: leaves, Join: Join{Steps: steps, Columns: columns}}, nil
}

// joinAlias falls back to the container name, so `orders.customerId` works
// for a side declared without an alias.
func joinAlias(s source) string {
	if s.alias != "" {
		return s.alias
	}
	return s.path[1].text
}

func (j joinPlanner) checkSources(containers []source) error {
	switch {
	case hasDuplicate(j.aliases):
		return unsupported("a join whose sides share an alias")
	case !keywordAt(j.toks, containers[0].firstTok-1, "FROM"):
		return unsupported("a container list mixed with other sources")
	case slices.ContainsFunc(containers[1:], func(c source) bool { return !c.joined }):
		return unsupported(shapeListWithJoin)
	}
	return nil
}

func hasDuplicate(names []string) bool {
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			return true
		}
		seen[name] = true
	}
	return false
}

// parseChain reads the ON clause of every joined container and returns the
// index just past the last one.
func (j joinPlanner) parseChain(containers []source) ([]JoinStep, int, error) {
	var steps []JoinStep
	end := containers[0].nextTok
	for joined := 1; joined < len(containers); joined++ {
		if err := j.checkJoinKeywords(j.toks[end:containers[joined].firstTok]); err != nil {
			return nil, end, err
		}
		step, next, err := j.parseStep(containers[joined].nextTok, joined)
		if err != nil {
			return nil, next, err
		}
		steps = append(steps, step)
		end = next
	}
	if keywordAt(j.toks, end, "JOIN") || keywordAt(j.toks, end, "INNER") {
		return nil, end, unsupported(shapePropertyJoin)
	}
	return steps, end, nil
}

// checkJoinKeywords accepts the tokens from the end of one side's clause to
// the next container: only `JOIN` or `INNER JOIN`. Anything else right after
// an ON equality extends that ON clause.
func (j joinPlanner) checkJoinKeywords(between []token) error {
	for i, tok := range between {
		switch {
		case tok.upper == "JOIN" || tok.upper == "INNER":
		case i == 0:
			return unsupported(shapeOn)
		default:
			return unsupported(shapePropertyJoin)
		}
	}
	return nil
}

// parseStep reads the ON clause of the container at index joined and
// returns the index just past it.
func (j joinPlanner) parseStep(i, joined int) (JoinStep, int, error) {
	if !keywordAt(j.toks, i, "ON") {
		return JoinStep{}, i, unsupported(shapeOn)
	}
	newSide, newPath, i, ok := j.parseFieldRef(i + 1)
	if !ok || i >= len(j.toks) || !isSymbol(j.toks[i], "=") {
		return JoinStep{}, i, unsupported(shapeOn)
	}
	earlierSide, earlierPath, i, ok := j.parseFieldRef(i + 1)
	if earlierSide == joined {
		newSide, newPath, earlierSide, earlierPath = earlierSide, earlierPath, newSide, newPath
	}
	if !ok || newSide != joined || earlierSide >= joined {
		return JoinStep{}, i, unsupported(shapeOn)
	}
	return JoinStep{Left: earlierSide, LeftKey: earlierPath, RightKey: newPath}, i, nil
}

func (j joinPlanner) leaf(side int, s source, filters []string) Leaf {
	text := "SELECT * FROM " + j.aliases[side]
	if len(filters) > 0 {
		text += " WHERE (" + strings.Join(filters, ") AND (") + ")"
	}
	return Leaf{
		Alias:    j.aliases[side],
		Query:    adapter.Query{Text: text, Scope: scopeOf(s)},
		Filtered: len(filters) > 0,
	}
}

// parseProjection reads the SELECT list that ends at the FROM token.
func (j joinPlanner) parseProjection(from int) ([]JoinColumn, error) {
	if !keywordAt(j.toks, 0, "SELECT") {
		return nil, unsupported(shapeProjection)
	}
	items := j.toks[1:from]
	if len(items) == 1 && isSymbol(items[0], "*") {
		return nil, nil
	}
	var columns []JoinColumn
	headers := map[string]bool{}
	for i := 0; ; i++ {
		column, next, ok := j.parseColumn(items, i)
		header := j.header(column)
		if !ok || headers[header] {
			return nil, unsupported(shapeProjection)
		}
		headers[header] = true
		columns = append(columns, column)
		if next == len(items) {
			return columns, nil
		}
		if items[next].kind != tokComma {
			return nil, unsupported(shapeProjection)
		}
		i = next
	}
}

// parseColumn reads `alias.field`, then an optional `AS name` or bare name,
// and returns the index just past it.
func (j joinPlanner) parseColumn(items []token, i int) (JoinColumn, int, bool) {
	if i+2 >= len(items) || items[i+1].kind != tokDot || items[i+2].kind != tokIdent {
		return JoinColumn{}, i, false
	}
	side, ok := j.side(items[i])
	column := JoinColumn{Side: side, Field: items[i+2].text}
	i += 3
	if keywordAt(items, i, "AS") {
		i++
		if i >= len(items) || items[i].kind != tokIdent {
			return JoinColumn{}, i, false
		}
	}
	if i < len(items) && items[i].kind == tokIdent && !keywords[items[i].upper] {
		column.As = items[i].text
		i++
	}
	return column, i, ok
}

// header is the column's name in the merged result.
func (j joinPlanner) header(column JoinColumn) string {
	return columnHeader(j.aliases[column.Side], column)
}

func columnHeader(alias string, column JoinColumn) string {
	if column.As != "" {
		return column.As
	}
	return alias + "." + column.Field
}

// parseFieldRef reads `alias.field[.field...]`.
func (j joinPlanner) parseFieldRef(i int) (side int, path []string, next int, ok bool) {
	if i >= len(j.toks) {
		return 0, nil, i, false
	}
	side, ok = j.side(j.toks[i])
	if !ok {
		return 0, nil, i, false
	}
	i++
	for i+1 < len(j.toks) && j.toks[i].kind == tokDot && j.toks[i+1].kind == tokIdent {
		path = append(path, j.toks[i+1].text)
		i += 2
	}
	return side, path, i, len(path) > 0
}

// parseWhere divides the conditions after ON between the sides, as the text
// each leaf query filters by. A condition is pushed down only whole: one that
// reads several sides would have to be evaluated client-side.
func (j joinPlanner) parseWhere(i int) ([][]string, error) {
	filters := make([][]string, len(j.aliases))
	if i == len(j.toks) {
		return filters, nil
	}
	switch tok := j.toks[i]; {
	case tok.kind == tokIdent && trailingClauses[tok.upper]:
		return filters, unsupported(tok.upper + " in a cross-container join")
	case !keywordAt(j.toks, i, "WHERE"):
		return filters, unsupported(shapeOn)
	}
	conjuncts, err := splitConjuncts(j.toks[i+1:])
	if err != nil {
		return filters, err
	}
	for _, conjunct := range conjuncts {
		if len(conjunct) == 0 {
			return filters, unsupported("an empty WHERE condition")
		}
		reads := j.sidesRead(conjunct)
		if len(reads) > 1 {
			return filters, unsupported("a WHERE condition over more than one side of a join")
		}
		condition := j.text[conjunct[0].start:conjunct[len(conjunct)-1].end]
		for side := range filters {
			if len(reads) == 0 || reads[side] {
				filters[side] = append(filters[side], condition)
			}
		}
	}
	return filters, nil
}

// splitConjuncts cuts conditions at its top-level ANDs. An OR would bind
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
			return nil, unsupported(conditions[i].upper + " in a cross-container join")
		}
	}
	if whole {
		return [][]token{conditions}, nil
	}
	var conjuncts [][]token
	start := 0
	for _, cut := range cuts {
		conjuncts = append(conjuncts, conditions[start:cut])
		start = cut + 1
	}
	return append(conjuncts, conditions[start:]), nil
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

func (j joinPlanner) sidesRead(toks []token) map[int]bool {
	reads := map[int]bool{}
	for i, tok := range toks {
		if side, ok := j.side(tok); ok && !followsDot(toks, i) {
			reads[side] = true
		}
	}
	return reads
}

func (j joinPlanner) side(tok token) (int, bool) {
	if tok.kind != tokIdent {
		return 0, false
	}
	for side, alias := range j.aliases {
		if tok.text == alias {
			return side, true
		}
	}
	return 0, false
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
