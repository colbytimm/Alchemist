package query

import (
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Shapes of a cross-container join the planner refuses.
const (
	shapeMultiWay   = "a join over more than two containers"
	shapeOn         = "an ON clause other than one equality between a field of each side"
	shapeProjection = "a join SELECT list other than * or alias.field [AS name] items"
)

// whereBreakers are the keywords that decide how a join's WHERE clause can be
// divided between its sides.
var whereBreakers = map[string]bool{
	"AND": true, "OR": true, "BETWEEN": true,
	"ORDER": true, "GROUP": true, "OFFSET": true, "LIMIT": true,
}

// joinPlanner reads `SELECT list FROM left JOIN right ON a.x = b.y [WHERE ...]`.
// Sides are indexed 0 for left and 1 for right throughout.
type joinPlanner struct {
	text    string
	toks    []token
	aliases [2]string
}

func planJoin(text string, toks []token, containers []source) (Plan, error) {
	if len(containers) != 2 || containers[0].joined {
		return Plan{}, unsupported(shapeMultiWay)
	}
	left, right := containers[0], containers[1]
	j := joinPlanner{text: text, toks: toks, aliases: [2]string{joinAlias(left), joinAlias(right)}}
	if err := j.checkSources(left, right); err != nil {
		return Plan{}, err
	}
	columns, err := j.parseProjection(left.firstTok - 1)
	if err != nil {
		return Plan{}, err
	}
	keys, next, err := j.parseOn(right.nextTok)
	if err != nil {
		return Plan{}, err
	}
	filters, err := j.parseWhere(next)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Merge:  HashJoin,
		Leaves: []Leaf{j.leaf(0, left, filters[0]), j.leaf(1, right, filters[1])},
		Join:   Join{LeftKey: keys[0], RightKey: keys[1], Columns: columns},
	}, nil
}

// joinAlias falls back to the container name, so `orders.customerId` works
// for a side declared without an alias.
func joinAlias(s source) string {
	if s.alias != "" {
		return s.alias
	}
	return s.path[1].text
}

func (j joinPlanner) checkSources(left, right source) error {
	switch {
	case j.aliases[0] == j.aliases[1]:
		return unsupported("a join whose sides share an alias")
	case !keywordAt(j.toks, left.firstTok-1, "FROM"):
		return unsupported("a container list mixed with other sources")
	}
	for _, tok := range j.toks[left.nextTok:right.firstTok] {
		if tok.upper != "JOIN" && tok.upper != "INNER" {
			return unsupported("JOIN ... IN alongside a cross-container join")
		}
	}
	return nil
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

// parseOn returns the key path of each side and the index just past the
// clause.
func (j joinPlanner) parseOn(i int) ([2][]string, int, error) {
	var keys [2][]string
	if !keywordAt(j.toks, i, "ON") {
		return keys, i, unsupported(shapeOn)
	}
	firstSide, firstPath, i, ok := j.parseFieldRef(i + 1)
	if !ok || i >= len(j.toks) || !isSymbol(j.toks[i], "=") {
		return keys, i, unsupported(shapeOn)
	}
	secondSide, secondPath, i, ok := j.parseFieldRef(i + 1)
	if !ok || firstSide == secondSide {
		return keys, i, unsupported(shapeOn)
	}
	keys[firstSide], keys[secondSide] = firstPath, secondPath
	return keys, i, nil
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
// reads both sides would have to be evaluated client-side.
func (j joinPlanner) parseWhere(i int) ([2][]string, error) {
	var filters [2][]string
	if i == len(j.toks) {
		return filters, nil
	}
	if !keywordAt(j.toks, i, "WHERE") {
		return filters, unsupported(shapeOn)
	}
	conjuncts, err := j.splitConjuncts(j.toks[i+1:])
	if err != nil {
		return filters, err
	}
	for _, conjunct := range conjuncts {
		if len(conjunct) == 0 {
			return filters, unsupported("an empty WHERE condition")
		}
		reads := j.sidesRead(conjunct)
		if reads[0] && reads[1] {
			return filters, unsupported("a WHERE condition over both sides of a join")
		}
		condition := j.text[conjunct[0].start:conjunct[len(conjunct)-1].end]
		for side := range filters {
			if !reads[1-side] {
				filters[side] = append(filters[side], condition)
			}
		}
	}
	return filters, nil
}

// splitConjuncts cuts conditions at its top-level ANDs. An OR would bind
// looser than those ANDs and BETWEEN brings an AND of its own, so either one
// keeps the clause whole.
func (j joinPlanner) splitConjuncts(conditions []token) ([][]token, error) {
	var cuts []int
	whole := false
	for _, i := range j.topLevelBreakers(conditions) {
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
func (j joinPlanner) topLevelBreakers(toks []token) []int {
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

func (j joinPlanner) sidesRead(toks []token) [2]bool {
	var reads [2]bool
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
