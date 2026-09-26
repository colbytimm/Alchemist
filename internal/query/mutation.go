package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// MutationKind is what a mutation statement does to each item it selects.
type MutationKind int

const (
	MutationUpdate MutationKind = iota + 1
	MutationDelete
)

// kindWords are the words a statement of one kind is named and refused in.
type kindWords struct {
	name, applied, ongoing string
	// head opens the statement, up to its target; statement is its keyword
	// with an article.
	head, statement string
	noWhere         string
	expectedWhere   string
}

var mutationWords = map[MutationKind]kindWords{
	MutationUpdate: {
		name: "update", applied: "updated", ongoing: "updating",
		head: "UPDATE", statement: "an UPDATE",
		noWhere:       "UPDATE needs a WHERE. To change every item, write WHERE true",
		expectedWhere: "expected WHERE, or a comma and another change",
	},
	MutationDelete: {
		name: "delete", applied: "deleted", ongoing: "deleting",
		head: "DELETE FROM", statement: "a DELETE",
		noWhere:       "DELETE needs a WHERE. To delete every item, write WHERE true",
		expectedWhere: "expected WHERE",
	},
}

func (k MutationKind) words() kindWords {
	if words, ok := mutationWords[k]; ok {
		return words
	}
	return mutationWords[MutationUpdate]
}

func (k MutationKind) String() string { return k.words().name }

// Applied is what an item the statement wrote was: "updated", "deleted".
func (k MutationKind) Applied() string { return k.words().applied }

// Ongoing is what the statement is doing while it writes: "updating",
// "deleting".
func (k MutationKind) Ongoing() string { return k.words().ongoing }

// Head is what a statement of kind k opens with, up to its target.
func (k MutationKind) Head() string { return k.words().head }

// MutationSyntaxError is a mutation statement that does not parse, at the
// line and column, both from one, where parsing stopped. Err is
// ErrMutationUnsupported for a shape that is SQL somewhere but not built
// here, and nil for text that is not a statement at all.
type MutationSyntaxError struct {
	Line    int
	Column  int
	Message string
	Err     error
}

func (e *MutationSyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

func (e *MutationSyntaxError) Unwrap() error { return e.Err }

var ErrMutationUnsupported = errors.New("not supported in an UPDATE or DELETE")

// Refusals a statement can meet while it is parsed.
const (
	errTargetForm   = "name the target as database.container: a write never depends on the catalog cursor"
	errLiteralOnly  = "SET takes a literal JSON value: a patch cannot read another field"
	errIncrement    = "increment is not built: it is the one patch operation that is not safe to run twice"
	errWholeItem    = "SET needs a field: replacing whole items is a REPLACE in a BEGIN BATCH"
	errUnsetWhole   = "UNSET needs a field, such as o.status"
	errArrayMoves   = "appending to and moving within arrays is not built"
	errOneStatement = "a buffer holds one statement"
	errUnbalanced   = "unbalanced parentheses in the WHERE"
	errDeleteField  = "DELETE removes whole items: removing a field is UPDATE … UNSET"
)

func oneTarget(k MutationKind) string {
	return k.words().statement + " has one target container"
}

func otherContainer(k MutationKind) string {
	return "the WHERE reads another container: choosing targets with a join is not built. " +
		"Run the join as a query and " + k.String() + " WHERE o.id IN (…)"
}

// refusedClauses may not appear outside parentheses anywhere in a mutation.
var refusedClauses = []string{"TOP", "ORDER", "OFFSET", "LIMIT", "RETURNING"}

// Mutation is an UPDATE or a DELETE as it was written: the container it
// writes, the name its items go by, what an update changes, and the
// condition that picks them, kept verbatim for the service to evaluate.
type Mutation struct {
	Kind        MutationKind
	Target      []string // [database, container]
	Alias       string
	Assignments []Assignment
	Removals    []FieldPath
	Where       string
	// EveryItem marks a WHERE that is exactly true.
	EveryItem bool
}

type Assignment struct {
	Path  FieldPath
	Value json.RawMessage
}

// FieldPath is a field of an item as a statement names it: its alias, then
// each name or array index under it.
type FieldPath struct {
	Alias string
	Steps []PathStep
}

// PathStep is a property name, or an array index when IsIndex is set.
type PathStep struct {
	Name    string
	Index   int
	IsIndex bool
}

// Dotted is the path written with dots alone, alias.a.b, and false for one
// that needs a bracket: an index, or a name that is no identifier.
func (p FieldPath) Dotted() (string, bool) {
	ref := p.Alias
	for _, step := range p.Steps {
		if step.IsIndex || !isIdentifier(step.Name) {
			return "", false
		}
		ref += "." + step.Name
	}
	return ref, true
}

// Parent is the path one step up, and false for a field at the top level,
// whose parent is the item.
func (p FieldPath) Parent() (FieldPath, bool) {
	last := len(p.Steps) - 1
	if last <= 0 {
		return FieldPath{}, false
	}
	return FieldPath{Alias: p.Alias, Steps: p.Steps[:last]}, true
}

// Pointer is the path as a JSON Pointer: /shipTo/region, /lines/0/qty.
func (p FieldPath) Pointer() string {
	var pointer strings.Builder
	for _, step := range p.Steps {
		pointer.WriteByte('/')
		if step.IsIndex {
			pointer.WriteString(strconv.Itoa(step.Index))
			continue
		}
		pointer.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(step.Name))
	}
	return pointer.String()
}

// String is the path as a statement writes it: o.shipTo.region,
// o["order-id"], o.lines[0].
func (p FieldPath) String() string {
	var text strings.Builder
	text.WriteString(p.Alias)
	for _, step := range p.Steps {
		switch {
		case step.IsIndex:
			text.WriteString("[" + strconv.Itoa(step.Index) + "]")
		case isIdentifier(step.Name):
			text.WriteString("." + step.Name)
		default:
			text.WriteString("[" + string(jsonString(step.Name)) + "]")
		}
	}
	return text.String()
}

// Operations is how many patch operations the statement makes of each
// item it selects.
func (m Mutation) Operations() int {
	return len(m.Assignments) + len(m.Removals)
}

// String writes m back as a statement that parses to m.
func (m Mutation) String() string {
	if m.Kind == MutationDelete {
		return fmt.Sprintf("DELETE FROM %s AS %s WHERE %s", strings.Join(m.Target, "."), m.Alias, m.Where)
	}
	text := fmt.Sprintf("UPDATE %s AS %s", strings.Join(m.Target, "."), m.Alias)
	if len(m.Assignments) > 0 {
		assignments := make([]string, 0, len(m.Assignments))
		for _, a := range m.Assignments {
			assignments = append(assignments, a.Path.String()+" = "+string(a.Value))
		}
		text += " SET " + strings.Join(assignments, ", ")
	}
	if len(m.Removals) > 0 {
		removals := make([]string, 0, len(m.Removals))
		for _, path := range m.Removals {
			removals = append(removals, path.String())
		}
		text += " UNSET " + strings.Join(removals, ", ")
	}
	return text + " WHERE " + m.Where
}

// IsMutation reports whether text is an UPDATE or a DELETE rather than a
// query: its first word is one of them, which no query starts with, or it
// opens WITH and its statement after the CTE list is one, which is refused
// when it is parsed.
func IsMutation(text string) bool {
	_, ok := MutationKindOf(text)
	return ok
}

// MutationKindOf is the kind of statement text starts, and false for text
// that is no mutation.
func MutationKindOf(text string) (MutationKind, bool) {
	return mutationKind(code(lex(text)))
}

func mutationKind(toks []token) (MutationKind, bool) {
	word := ""
	switch {
	case keywordAt(toks, 0, "WITH"):
		word = statementAfterCTEs(toks)
	case len(toks) > 0 && toks[0].kind == tokIdent:
		word = toks[0].upper
	}
	switch word {
	case "UPDATE":
		return MutationUpdate, true
	case "DELETE":
		return MutationDelete, true
	}
	return 0, false
}

// statementAfterCTEs is the word that opens the statement after a WITH's
// CTE list, each CTE read as <name> AS ( … ), and "" for a list that does
// not read so. A CTE's name may be any word, UPDATE and DELETE included.
func statementAfterCTEs(toks []token) string {
	for i := 1; ; i++ {
		if i+2 >= len(toks) || toks[i].kind != tokIdent || !keywordAt(toks, i+1, "AS") || !isSymbol(toks[i+2], "(") {
			return ""
		}
		i = matchingParen(toks, i+2) + 1
		switch {
		case i >= len(toks):
			return ""
		case toks[i].kind == tokIdent:
			return toks[i].upper
		case toks[i].kind != tokComma:
			return ""
		}
	}
}

// matchingParen is the index of the ) that closes the ( at open, or the
// last index when none does.
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
	return len(toks) - 1
}

// MutationTarget is the dotted path an UPDATE or a DELETE FROM names as
// its target, however many parts it has, and nil for text that names none.
func MutationTarget(text string) []string {
	toks := code(lex(text))
	start := 1
	switch {
	case keywordAt(toks, 0, "DELETE") && keywordAt(toks, 1, "FROM"):
		start = 2
	case !keywordAt(toks, 0, "UPDATE"):
		return nil
	}
	if len(toks) <= start || toks[start].kind != tokIdent {
		return nil
	}
	path := []string{toks[start].text}
	for i := start + 1; i+1 < len(toks) && toks[i].kind == tokDot && toks[i+1].kind == tokIdent; i += 2 {
		path = append(path, toks[i+1].text)
	}
	return path
}

// ParseMutation reads an UPDATE or a DELETE:
//
//	UPDATE <database>.<container> [[AS] <alias>]
//	  SET <path> = <value> [, ...] [UNSET <path> [, ...]] | UNSET <path> [, ...]
//	  WHERE <condition> [;]
//
//	DELETE FROM <database>.<container> [[AS] <alias>] WHERE <condition> [;]
//
// A statement without its WHERE does not parse, so a half-typed one can
// never select a whole container. The condition is never parsed: the
// service evaluates it, in the words it was written in.
func ParseMutation(text string) (Mutation, error) {
	p := &mutationParser{statementReader: newStatementReader(text, mutationSyntaxError)}
	return p.parse()
}

func mutationSyntaxError(line, column int, message string) error {
	return &MutationSyntaxError{Line: line, Column: column, Message: message}
}

type mutationParser struct {
	statementReader
	kind MutationKind
}

func (p *mutationParser) parse() (Mutation, error) {
	kind, ok := mutationKind(p.toks)
	if !ok {
		return Mutation{}, p.fail("a statement that writes starts UPDATE or DELETE")
	}
	p.kind = kind
	if err := p.refuseClauses(); err != nil {
		return Mutation{}, err
	}
	if keywordAt(p.toks, 0, "WITH") {
		return Mutation{}, p.unsupported(oneTarget(kind))
	}
	m := Mutation{Kind: kind}
	var err error
	if kind == MutationDelete {
		err = p.parseDeleteHead(&m)
	} else {
		err = p.parseUpdateHead(&m)
	}
	if err != nil {
		return Mutation{}, err
	}
	if m.Where, err = p.parseWhere(m.Alias); err != nil {
		return Mutation{}, err
	}
	m.EveryItem = strings.EqualFold(withoutOuterParens(m.Where), "true")
	return m, nil
}

// withoutOuterParens strips the parentheses that enclose all of a balanced
// condition, however many pairs: ((true)) is true.
func withoutOuterParens(condition string) string {
	for {
		toks := code(lex(condition))
		n := len(toks)
		if n < 2 || !isSymbol(toks[0], "(") || matchingParen(toks, 0) != n-1 {
			return strings.TrimSpace(condition)
		}
		condition = condition[toks[0].end:toks[n-1].start]
	}
}

// parseUpdateHead reads everything of an update before its WHERE.
func (p *mutationParser) parseUpdateHead(m *Mutation) error {
	p.keyword("UPDATE")
	var err error
	if m.Target, err = p.parseTarget(); err != nil {
		return err
	}
	if m.Alias, err = p.parseAlias(); err != nil {
		return err
	}
	return p.parseChanges(m)
}

// parseDeleteHead reads everything of a delete before its WHERE: FROM, the
// one target and its alias. A delete names no field and no second source.
func (p *mutationParser) parseDeleteHead(m *Mutation) error {
	p.keyword("DELETE")
	if !keywordAt(p.toks, p.i, "FROM") {
		return p.refuseDeleteWithoutFrom()
	}
	p.i++
	var err error
	if m.Target, err = p.parseTarget(); err != nil {
		return err
	}
	if p.startsDeleteSource() {
		return p.unsupported(oneTarget(p.kind))
	}
	if m.Alias, err = p.parseAlias(); err != nil {
		return err
	}
	if p.startsDeleteSource() {
		return p.unsupported(oneTarget(p.kind))
	}
	return nil
}

// startsDeleteSource reports a USING, the other way SQL gives a delete a
// second source to read, or any source an update refuses.
func (p *mutationParser) startsDeleteSource() bool {
	return keywordAt(p.toks, p.i, "USING") || p.startsSource()
}

// refuseDeleteWithoutFrom names what came between DELETE and a FROM ahead
// of the WHERE, or shows the statement with the FROM it lacks.
func (p *mutationParser) refuseDeleteWithoutFrom() error {
	head := p.toks[p.i:p.whereIndex()]
	from := slices.IndexFunc(head, func(tok token) bool { return tok.kind == tokIdent && tok.upper == "FROM" })
	switch {
	case from < 0:
		return p.fail("DELETE needs FROM: DELETE FROM " + p.headText(head) + " …")
	case slices.ContainsFunc(head[:from], func(tok token) bool { return tok.kind == tokDot }):
		return p.fail(errDeleteField)
	}
	return p.unsupported(oneTarget(p.kind))
}

// whereIndex is the index of the first WHERE from p.i on, or the end of
// the tokens.
func (p *mutationParser) whereIndex() int {
	where := slices.IndexFunc(p.toks[p.i:], func(tok token) bool { return tok.kind == tokIdent && tok.upper == "WHERE" })
	if where < 0 {
		return len(p.toks)
	}
	return p.i + where
}

// headText writes head as the statement had it, and the WHERE after it.
func (p *mutationParser) headText(head []token) string {
	where := ""
	if p.i+len(head) < len(p.toks) {
		where = " WHERE"
	}
	if n := len(head); n > 0 && isSymbol(head[n-1], ";") {
		head = head[:n-1]
	}
	text := "database.container"
	if len(head) > 0 {
		text = p.text[head[0].start:head[len(head)-1].end]
	}
	return text + where
}

// refuseClauses refuses a clause that would limit or order the items
// written, anywhere outside parentheses: a subquery may use them.
func (p *mutationParser) refuseClauses() error {
	depth := 0
	for i, tok := range p.toks {
		switch {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		case depth <= 0 && tok.kind == tokIdent && slices.Contains(refusedClauses, tok.upper) && !followsDot(p.toks, i):
			p.i = i
			return p.unsupported(tok.upper + " in " + p.kind.words().statement)
		}
	}
	return nil
}

func (p *mutationParser) parseTarget() ([]string, error) {
	start := p.i
	name, ok := p.identifier()
	if !ok {
		return nil, p.fail(errTargetForm)
	}
	target := []string{name}
	for p.at(tokDot) {
		p.i++
		if name, ok = p.identifier(); !ok {
			return nil, p.fail(errTargetForm)
		}
		target = append(target, name)
	}
	if len(target) != 2 {
		p.i = start
		return nil, p.fail(errTargetForm)
	}
	if p.at(tokComma) {
		return nil, p.unsupported(oneTarget(p.kind))
	}
	return target, nil
}

// parseAlias reads AS <alias> or a bare alias, neither of which a clause
// word can be. A comma after it would name a second target.
func (p *mutationParser) parseAlias() (string, error) {
	named := p.keyword("AS")
	if !p.at(tokIdent) || !isAliasWord(p.toks[p.i].upper) {
		if named {
			return "", p.fail("expected an alias after AS")
		}
		return defaultAlias, nil
	}
	alias := p.toks[p.i].text
	p.i++
	if p.at(tokComma) {
		return "", p.unsupported(oneTarget(p.kind))
	}
	return alias, nil
}

func isAliasWord(upper string) bool {
	return !keywords[upper] && !mutationKeywords[upper] && !joinModifiers[upper]
}

func (p *mutationParser) parseChanges(m *Mutation) error {
	if p.startsSource() {
		return p.unsupported(oneTarget(p.kind))
	}
	var err error
	switch {
	case p.keyword("SET"):
		if m.Assignments, err = p.parseAssignments(m.Alias); err != nil {
			return err
		}
		if p.keyword("UNSET") {
			m.Removals, err = p.parseRemovals(m.Alias)
		}
	case p.keyword("UNSET"):
		m.Removals, err = p.parseRemovals(m.Alias)
	case keywordAt(p.toks, p.i, "INCREMENT"):
		err = p.unsupported(errIncrement)
	default:
		err = p.fail("expected SET or UNSET")
	}
	return err
}

// startsSource reports a FROM or a JOIN where the changes belong: a second
// source for the statement to read.
func (p *mutationParser) startsSource() bool {
	return keywordAt(p.toks, p.i, "FROM") || keywordAt(p.toks, p.i, "JOIN") ||
		p.at(tokIdent) && joinModifiers[p.toks[p.i].upper]
}

func (p *mutationParser) parseAssignments(alias string) ([]Assignment, error) {
	var assignments []Assignment
	for {
		assignment, err := p.parseAssignment(alias)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
		if !p.at(tokComma) {
			return assignments, nil
		}
		p.i++
	}
}

func (p *mutationParser) parseAssignment(alias string) (Assignment, error) {
	if keywordAt(p.toks, p.i, "INCREMENT") {
		return Assignment{}, p.unsupported(errIncrement)
	}
	path, err := p.parseAliasedPath(alias, errWholeItem)
	if err != nil {
		return Assignment{}, err
	}
	if (isSymbolAt(p.toks, p.i, "+") || isSymbolAt(p.toks, p.i, "-")) && isSymbolAt(p.toks, p.i+1, "=") {
		return Assignment{}, p.unsupported(errIncrement)
	}
	if !p.symbol("=") {
		return Assignment{}, p.fail("expected = after " + path.String())
	}
	start := p.i
	value, err := p.parseValue()
	if err != nil {
		return Assignment{}, err
	}
	if p.startsSource() {
		return Assignment{}, p.unsupported(oneTarget(p.kind))
	}
	if !p.endsChange() {
		p.i = start
		return Assignment{}, p.unsupported(errLiteralOnly)
	}
	return Assignment{Path: path, Value: value}, nil
}

// endsChange reports whether the token at p.i may follow a change: another
// change, the removals, or the condition.
func (p *mutationParser) endsChange() bool {
	return p.done() || p.at(tokComma) || isSymbolAt(p.toks, p.i, ";") ||
		keywordAt(p.toks, p.i, "UNSET") || keywordAt(p.toks, p.i, "WHERE")
}

func (p *mutationParser) parseRemovals(alias string) ([]FieldPath, error) {
	var removals []FieldPath
	for {
		path, err := p.parseAliasedPath(alias, errUnsetWhole)
		if err != nil {
			return nil, err
		}
		removals = append(removals, path)
		if !p.at(tokComma) {
			return removals, nil
		}
		p.i++
	}
}

// parseAliasedPath reads a path under alias. whole is the refusal of the
// alias alone, which names the whole item rather than a field of it.
func (p *mutationParser) parseAliasedPath(alias, whole string) (FieldPath, error) {
	start := p.i
	path, err := p.parseFieldPath()
	if err != nil {
		return FieldPath{}, err
	}
	if path.Alias != alias {
		p.i = start
		return FieldPath{}, p.fail(fmt.Sprintf("%s: paths start with the target's alias %s", path, alias))
	}
	if len(path.Steps) == 0 {
		p.i = start
		return FieldPath{}, p.fail(whole)
	}
	return path, nil
}

// parseFieldPath reads <alias> { .name | ["name"] | [index] }.
func (p *mutationParser) parseFieldPath() (FieldPath, error) {
	alias, ok := p.identifier()
	if !ok {
		return FieldPath{}, p.fail("expected a field, such as c.status")
	}
	path := FieldPath{Alias: alias}
	for {
		var step PathStep
		var err error
		switch {
		case p.at(tokDot):
			p.i++
			if step.Name, ok = p.identifier(); !ok {
				return FieldPath{}, p.fail("expected a field name after .")
			}
		case isSymbolAt(p.toks, p.i, "["):
			if step, err = p.parseBracket(); err != nil {
				return FieldPath{}, err
			}
		default:
			return path, nil
		}
		path.Steps = append(path.Steps, step)
	}
}

// parseBracket reads ["name"] or [index]. Only a written, non-negative index
// is a place: the rest would move items within an array.
func (p *mutationParser) parseBracket() (PathStep, error) {
	p.i++
	var step PathStep
	switch {
	case p.at(tokString):
		name, err := p.stringLiteral()
		if err != nil {
			return PathStep{}, err
		}
		step.Name = name
	case p.at(tokNumber):
		index, err := strconv.Atoi(p.tokenText())
		if err != nil {
			return PathStep{}, p.fail("an array index is a whole number")
		}
		step.Index, step.IsIndex = index, true
		p.i++
	case isSymbolAt(p.toks, p.i, "-"), isSymbolAt(p.toks, p.i, "]"):
		return PathStep{}, p.unsupported(errArrayMoves)
	default:
		return PathStep{}, p.fail(`expected a quoted name or an index inside [ ]`)
	}
	if !p.symbol("]") {
		return PathStep{}, p.fail("expected ]")
	}
	return step, nil
}

// parseValue reads a literal as JSON. Anything else would ask the patch to
// read the item, which a patch cannot.
func (p *mutationParser) parseValue() (json.RawMessage, error) {
	switch {
	case p.at(tokString):
		value, err := p.stringLiteral()
		if err != nil {
			return nil, err
		}
		return jsonString(value), nil
	case p.keyword("TRUE"):
		return json.RawMessage("true"), nil
	case p.keyword("FALSE"):
		return json.RawMessage("false"), nil
	case p.keyword("NULL"):
		return json.RawMessage("null"), nil
	case isSymbolAt(p.toks, p.i, "{"):
		return p.jsonValue("{", "}", "a JSON object")
	case isSymbolAt(p.toks, p.i, "["):
		return p.jsonValue("[", "]", "a JSON array")
	}
	return p.parseNumber()
}

func (p *mutationParser) parseNumber() (json.RawMessage, error) {
	start := p.i
	number := ""
	if p.symbol("-") {
		number = "-"
	}
	if !p.at(tokNumber) {
		p.i = start
		return nil, p.unsupported(errLiteralOnly)
	}
	number += p.tokenText()
	p.i++
	if !json.Valid([]byte(number)) {
		p.i = start
		return nil, p.fail(number + " is not a number JSON reads")
	}
	return json.RawMessage(number), nil
}

// parseWhere keeps the condition's text as written, from WHERE to the end
// of the statement.
func (p *mutationParser) parseWhere(alias string) (string, error) {
	if p.done() || isSymbolAt(p.toks, p.i, ";") {
		return "", p.fail(p.kind.words().noWhere)
	}
	if p.startsSource() {
		return "", p.unsupported(oneTarget(p.kind))
	}
	if !p.keyword("WHERE") {
		return "", p.fail(p.kind.words().expectedWhere)
	}
	start, end := p.i, p.statementEnd()
	if end == start {
		return "", p.fail("WHERE needs a condition")
	}
	condition := p.toks[start:end]
	if open := slices.IndexFunc(condition, func(tok token) bool { return tok.open }); open >= 0 {
		p.i = start + open
		return "", p.fail("this string is never closed")
	}
	if err := p.refuseUnbalanced(condition, start); err != nil {
		return "", err
	}
	if err := p.refuseOtherContainers(condition, start, alias); err != nil {
		return "", err
	}
	if end+1 < len(p.toks) {
		p.i = end + 1
		return "", p.fail(errOneStatement)
	}
	return p.conditionText(start, end), nil
}

// conditionText is the condition as written, each comment in it replaced
// by a space. The text is sent inside a wrapper of its own, and a comment
// the service ends somewhere Alchemist does not would carry the rest of
// the line out of that wrapper.
func (p *mutationParser) conditionText(start, end int) string {
	from, to := p.toks[start].start, p.toks[end-1].end
	var text strings.Builder
	last := from
	for _, tok := range lex(p.text[:to]) {
		if tok.kind != tokComment || tok.start < from {
			continue
		}
		text.WriteString(p.text[last:tok.start])
		text.WriteByte(' ')
		last = tok.end
	}
	text.WriteString(p.text[last:to])
	return text.String()
}

// statementEnd is the index of the semicolon that ends the statement, or of
// the end of the text.
func (p *mutationParser) statementEnd() int {
	depth := 0
	for i := p.i; i < len(p.toks); i++ {
		switch tok := p.toks[i]; {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		case depth <= 0 && isSymbol(tok, ";"):
			return i
		}
	}
	return len(p.toks)
}

// refuseUnbalanced refuses a condition whose parentheses do not pair up.
// The condition is sent inside a pair of its own, (<condition>), with more
// conditions ANDed after it; a ) that closes that pair early would let the
// rest of the text escape it: "o.a = 1) OR (true" matches every item.
func (p *mutationParser) refuseUnbalanced(condition []token, offset int) error {
	depth := 0
	for i, tok := range condition {
		switch {
		case isSymbol(tok, "("):
			depth++
		case isSymbol(tok, ")"):
			depth--
		}
		if depth < 0 {
			p.i = offset + i
			return p.fail(errUnbalanced)
		}
	}
	if depth != 0 {
		p.i = offset + len(condition) - 1
		return p.fail(errUnbalanced)
	}
	return nil
}

// refuseOtherContainers refuses a condition that reads a database.container
// source. A source rooted at the statement's alias is one of its items'
// own arrays, not another container.
func (p *mutationParser) refuseOtherContainers(condition []token, offset int, alias string) error {
	sources := parseTokens(condition)
	sources.aliases[alias] = true
	if others := sources.containerSources(); len(others) > 0 {
		p.i = offset + others[0].firstTok
		return p.unsupported(otherContainer(p.kind))
	}
	return nil
}

// unsupported refuses, at the token parsing stopped on, a shape that is SQL
// somewhere but is not built here.
func (p *mutationParser) unsupported(shape string) error {
	err := p.fail(shape)
	var syntax *MutationSyntaxError
	if errors.As(err, &syntax) {
		syntax.Err = ErrMutationUnsupported
	}
	return err
}
