package query

import (
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// joinPlanner lowers a simulated body. An input is the index of its source in
// written order throughout.
type joinPlanner struct {
	*planner
	frag   fragment
	body   body
	owner  string
	inputs []plannedInput
	// optional maps each input some step can pad to the kind of the first
	// such step.
	optional map[int]JoinKind
}

type plannedInput struct {
	alias   string
	cte     *cte // nil for a container
	scope   []string
	applies []Apply
	filters []string
}

// aliases are the names the input binds: its own, then its APPLYs'.
func (in plannedInput) aliases() []string {
	names := []string{in.alias}
	for _, apply := range in.applies {
		names = append(names, apply.Alias)
	}
	return names
}

func (p *planner) simulated(f fragment, owner string) (lowered, error) {
	b, err := parseBody(f)
	if err != nil {
		return lowered{}, err
	}
	j := &joinPlanner{planner: p, frag: f, body: b, owner: owner, optional: map[int]JoinKind{}}
	if err := j.resolveInputs(); err != nil {
		return lowered{}, err
	}
	if c, ok := j.renamedCTE(); ok {
		return lowered{node: p.rowsOf(c), fields: c.fields, values: c.values}, nil
	}
	return j.lower()
}

func (j *joinPlanner) lower() (lowered, error) {
	if err := j.checkReadsObjects(); err != nil {
		return lowered{}, err
	}
	steps, err := j.parseSteps()
	if err != nil {
		return lowered{}, err
	}
	j.classifyOptional(steps)
	if err := j.checkApplyPlacement(steps); err != nil {
		return lowered{}, err
	}
	absent, err := j.splitWhere()
	if err != nil {
		return lowered{}, err
	}
	columns, err := j.parseProjection()
	if err != nil {
		return lowered{}, err
	}
	join := &Join{Inputs: j.sources(), Steps: steps, Columns: columns, Absent: absent}
	if j.owner == "" {
		return lowered{node: join}, nil
	}
	return j.flatten(join)
}

// resolveInputs binds each input to a CTE or a container. A one-part name
// that is no CTE is the container in scope only as the first source, as
// Cosmos reads `FROM c`; joined, it is most likely a mistyped CTE or a
// container missing its database.
func (j *joinPlanner) resolveInputs() error {
	for k, text := range j.body.inputs {
		input := plannedInput{alias: text.name()}
		switch c, isCTE := j.visible(text.path[0].text); {
		case len(text.path) == 1 && isCTE:
			input.cte = c
		case len(text.path) == 2:
			input.scope = []string{text.path[0].text, text.path[1].text}
		case k > 0:
			return notASource(text.path[0].text)
		}
		j.inputs = append(j.inputs, input)
	}
	for k, text := range j.body.inputs {
		applies, err := j.resolveApplies(k, text.applies)
		if err != nil {
			return err
		}
		j.inputs[k].applies = applies
	}
	if hasDuplicate(j.allAliases(len(j.inputs))) {
		return unsupported("a join whose sides share an alias")
	}
	return nil
}

// allAliases are the names the first n inputs bind, in written order.
func (j *joinPlanner) allAliases(n int) []string {
	var names []string
	for _, input := range j.inputs[:n] {
		names = append(names, input.aliases()...)
	}
	return names
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

// resolveApplies binds each APPLY to an array of the input it follows: of
// the input's own items, or of an element an earlier APPLY of it binds.
func (j *joinPlanner) resolveApplies(k int, texts []applyText) ([]Apply, error) {
	bound := []string{j.inputs[k].alias}
	var applies []Apply
	for _, text := range texts {
		array, ok := fieldRef(text.array)
		if !ok || !slices.Contains(bound, array.Alias) {
			return nil, unsupported("APPLY must follow the source it reads")
		}
		applies = append(applies, Apply{Alias: text.alias, Array: array, Outer: text.outer})
		bound = append(bound, text.alias)
	}
	return applies, nil
}

// renamedCTE is the CTE a body reads whole and unchanged: `SELECT * FROM
// name` and nothing else.
func (j *joinPlanner) renamedCTE() (*cte, bool) {
	b := j.body
	only := len(b.inputs) == 1 && len(b.inputs[0].applies) == 0 && !b.hasWhere
	if !only || j.inputs[0].cte == nil || len(b.projection) != 1 || !isSymbol(b.projection[0], "*") {
		return nil, false
	}
	return j.inputs[0].cte, true
}

func (j *joinPlanner) checkReadsObjects() error {
	for _, input := range j.inputs {
		if input.cte != nil && input.cte.values {
			return unsupported("SELECT VALUE in the CTE " + input.cte.name + ": a CTE the main query joins must return objects")
		}
	}
	return nil
}

func (j *joinPlanner) parseSteps() ([]JoinStep, error) {
	var steps []JoinStep
	for i, text := range j.body.steps {
		if text.kind == CrossJoin && len(j.body.steps) > 1 {
			return nil, unsupported("CROSS JOIN in a chain: put one side in a CTE")
		}
		step, err := j.parseStep(i+1, text)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// parseStep reads the ON clause of the input at index joined: its equality,
// then the conditions that filter the joined input.
func (j *joinPlanner) parseStep(joined int, text stepText) (JoinStep, error) {
	step := JoinStep{Kind: text.kind}
	if text.kind == CrossJoin {
		return step, nil
	}
	conjuncts, err := splitOn(text.on)
	if err != nil {
		return JoinStep{}, err
	}
	step.Left, step.LeftKey, step.RightKey, err = j.parseEquality(joined, conjuncts[0])
	if err != nil {
		return JoinStep{}, err
	}
	for _, extra := range conjuncts[1:] {
		if err := j.pushOnCondition(joined, text.kind, extra); err != nil {
			return JoinStep{}, err
		}
	}
	return step, nil
}

// parseEquality reads `ref = ref`, one side through the joined input's
// alias and the other through an alias an earlier input binds.
func (j *joinPlanner) parseEquality(joined int, toks []token) (int, FieldRef, FieldRef, error) {
	eq := slices.IndexFunc(toks, func(t token) bool { return isSymbol(t, "=") })
	if eq < 0 {
		return 0, FieldRef{}, FieldRef{}, unsupported(shapeOn)
	}
	newRef, okNew := fieldRef(toks[:eq])
	earlierRef, okEarlier := fieldRef(toks[eq+1:])
	if earlierRef.Alias == j.inputs[joined].alias {
		newRef, earlierRef = earlierRef, newRef
	}
	earlier, bound := j.binder(earlierRef.Alias, joined)
	if !okNew || !okEarlier || newRef.Alias != j.inputs[joined].alias || !bound {
		return 0, FieldRef{}, FieldRef{}, unsupported(shapeOn)
	}
	return earlier, earlierRef, newRef, nil
}

// binder is the input among the first n that binds alias.
func (j *joinPlanner) binder(alias string, n int) (int, bool) {
	for k, input := range j.inputs[:n] {
		if slices.Contains(input.aliases(), alias) {
			return k, true
		}
	}
	return 0, false
}

// pushOnCondition sends an ON condition past the equality to the leaf of
// the joined input: filtering that input before the join is what it means,
// as long as the join may drop the input's failing rows.
func (j *joinPlanner) pushOnCondition(joined int, kind JoinKind, condition []token) error {
	reads := aliasesRead(condition, j.allAliases(len(j.inputs)))
	joinedOnly := len(reads) == 0 || len(reads) == 1 && reads[0] == j.inputs[joined].alias
	switch {
	case len(condition) == 0, len(reads) > 1, !joinedOnly && kind == InnerJoin:
		return unsupported(shapeOn)
	case !joinedOnly, kind == RightOuterJoin, kind == FullOuterJoin:
		return unsupported("an ON condition on the preserved side of " + kind.String() + " JOIN")
	case j.inputs[joined].cte != nil:
		return unsupported("an ON condition on the CTE " + j.inputs[joined].cte.name + ": filter inside the CTE")
	}
	j.inputs[joined].filters = append(j.inputs[joined].filters, j.textOf(condition))
	return nil
}

func (j *joinPlanner) textOf(toks []token) string {
	return j.frag.text[toks[0].start:toks[len(toks)-1].end]
}

// classifyOptional marks the inputs some step can pad: the joined input of a
// LEFT step, every earlier one of a RIGHT step, both for FULL.
func (j *joinPlanner) classifyOptional(steps []JoinStep) {
	mark := func(k int, kind JoinKind) {
		if _, ok := j.optional[k]; !ok {
			j.optional[k] = kind
		}
	}
	for i, step := range steps {
		joined := i + 1
		switch step.Kind {
		case LeftOuterJoin:
			mark(joined, step.Kind)
		case RightOuterJoin, FullOuterJoin:
			for k := range joined {
				mark(k, step.Kind)
			}
			if step.Kind == FullOuterJoin {
				mark(joined, step.Kind)
			}
		}
	}
}

// checkApplyPlacement refuses a CROSS APPLY on the joined input of a LEFT or
// FULL step: SQL applies it after the join, dropping the padded rows, and
// expanding the input before the join would keep them.
func (j *joinPlanner) checkApplyPlacement(steps []JoinStep) error {
	for i, step := range steps {
		if step.Kind != LeftOuterJoin && step.Kind != FullOuterJoin {
			continue
		}
		for _, apply := range j.inputs[i+1].applies {
			if !apply.Outer {
				return unsupported("CROSS APPLY on the optional side of " + step.Kind.String() + " JOIN: use OUTER APPLY")
			}
		}
	}
	return nil
}

// splitWhere divides the WHERE conditions between the inputs, as the text
// each leaf query filters by, and returns the aliases the absent-side test
// asks for. A condition is pushed down only whole, and only to an input no
// step pads: one that reads several inputs, or a padded one, would have to
// be evaluated client-side.
func (j *joinPlanner) splitWhere() ([]string, error) {
	if !j.body.hasWhere {
		return nil, nil
	}
	conjuncts, err := splitConjuncts(j.body.where)
	if err != nil {
		return nil, err
	}
	var absent []string
	for _, conjunct := range conjuncts {
		if alias, ok := absentTest(conjunct); ok && j.canBeAbsent(alias) {
			absent = append(absent, alias)
			continue
		}
		if err := j.pushWhereCondition(conjunct); err != nil {
			return nil, err
		}
	}
	return absent, nil
}

func (j *joinPlanner) canBeAbsent(alias string) bool {
	k, bound := j.binder(alias, len(j.inputs))
	if !bound {
		return false
	}
	if _, optional := j.optional[k]; optional {
		return true
	}
	return slices.ContainsFunc(j.inputs[k].applies, func(a Apply) bool { return a.Alias == alias && a.Outer })
}

func (j *joinPlanner) pushWhereCondition(conjunct []token) error {
	if len(conjunct) == 0 {
		return unsupported("an empty WHERE condition")
	}
	reads := aliasesRead(conjunct, j.allAliases(len(j.inputs)))
	switch len(reads) {
	case 0:
		return j.pushEverywhere(conjunct)
	case 1:
	default:
		return unsupported("a WHERE condition over more than one side of a join")
	}
	k, _ := j.binder(reads[0], len(j.inputs))
	input := j.inputs[k]
	kind, optional := j.optional[k]
	switch {
	case reads[0] != input.alias:
		return unsupported("a WHERE condition on an APPLY alias: filter elements in a CTE with JOIN ... IN")
	case optional:
		return optionalCondition(kind, conjunct)
	case input.cte != nil:
		return unsupported("a WHERE condition on the CTE " + input.cte.name + ": filter inside the CTE")
	}
	j.inputs[k].filters = append(j.inputs[k].filters, j.textOf(conjunct))
	return nil
}

// optionalCondition refuses a condition on a padded input. Pushed down, it
// would filter that input before the join and keep the padded rows; SQL
// applies it after the join and drops them.
func optionalCondition(kind JoinKind, conjunct []token) error {
	shape := "a WHERE condition on the optional side of " + kind.String() + " JOIN: "
	switch _, isDefined := definedTest(conjunct); {
	case isDefined:
		return unsupported(shape + "use INNER JOIN")
	case kind == LeftOuterJoin:
		return unsupported(shape + "write it in ON to filter that side before the join, or use INNER JOIN")
	}
	return unsupported(shape + "filter that side in a CTE, or use INNER JOIN")
}

// pushEverywhere filters every input by a condition that reads none of them.
// A CTE input has no leaf of this body to take it.
func (j *joinPlanner) pushEverywhere(conjunct []token) error {
	for k, input := range j.inputs {
		if input.cte != nil {
			return unsupported("a WHERE condition on no source beside the CTE " + input.cte.name + ": filter inside the CTE")
		}
		j.inputs[k].filters = append(j.inputs[k].filters, j.textOf(conjunct))
	}
	return nil
}

// parseProjection reads the SELECT list: * or `alias.field [AS name]` items,
// where an APPLY alias may also be named bare.
func (j *joinPlanner) parseProjection() ([]JoinColumn, error) {
	items := j.body.projection
	if len(items) == 1 && isSymbol(items[0], "*") {
		return nil, nil
	}
	var columns []JoinColumn
	headers := map[string]bool{}
	for _, item := range splitTopLevel(items) {
		column, err := j.parseColumn(item)
		if err != nil {
			return nil, err
		}
		header := columnHeader(column)
		if headers[header] {
			return nil, unsupported(shapeProjection + hintCTE)
		}
		headers[header] = true
		columns = append(columns, column)
	}
	return columns, nil
}

func (j *joinPlanner) parseColumn(item []token) (JoinColumn, error) {
	end := pathEnd(item, 0)
	column, ok := j.columnAt(item[:end])
	if !ok {
		return JoinColumn{}, unsupported(shapeProjection + hintCTE)
	}
	rest := item[end:]
	renamed := keywordAt(rest, 0, "AS")
	if renamed {
		rest = rest[1:]
	}
	switch {
	case len(rest) == 1 && rest[0].kind == tokIdent && !keywords[rest[0].upper]:
		column.As = rest[0].text
	case len(rest) > 0, renamed:
		return JoinColumn{}, unsupported(shapeProjection + hintCTE)
	}
	return column, j.checkKnownField(column)
}

// columnAt reads `alias.field`, or an APPLY alias alone.
func (j *joinPlanner) columnAt(path []token) (JoinColumn, bool) {
	if len(path) == 0 || len(path) > 3 {
		return JoinColumn{}, false
	}
	k, bound := j.binder(path[0].text, len(j.inputs))
	if !bound {
		return JoinColumn{}, false
	}
	column := JoinColumn{Side: k, Alias: path[0].text}
	if len(path) == 3 {
		column.Field = path[2].text
		return column, true
	}
	return column, column.Alias != j.inputs[k].alias
}

// checkKnownField refuses a column a CTE's own SELECT list does not name.
func (j *joinPlanner) checkKnownField(column JoinColumn) error {
	input := j.inputs[column.Side]
	if input.cte == nil || input.cte.fields == nil || column.Alias != input.alias {
		return nil
	}
	if !slices.Contains(input.cte.fields, column.Field) {
		return unsupported(input.cte.name + " has no column " + column.Field)
	}
	return nil
}

// sources makes each input a Source: a CTE's rows, or a new leaf reading the
// container under its alias with the conditions pushed down to it.
func (j *joinPlanner) sources() []Source {
	sources := make([]Source, 0, len(j.inputs))
	for _, input := range j.inputs {
		source := Source{Alias: input.alias, Applies: input.applies}
		if input.cte != nil {
			source.Rows, source.Fields = j.rowsOf(input.cte), input.cte.fields
		} else {
			source.Rows = &Scan{Leaf: j.addLeaf(j.leaf(input))}
		}
		sources = append(sources, source)
	}
	return sources
}

func (j *joinPlanner) leaf(input plannedInput) Leaf {
	text := "SELECT * FROM " + input.alias
	if len(input.filters) > 0 {
		text += " WHERE (" + strings.Join(input.filters, ") AND (") + ")"
	}
	return Leaf{
		Alias:    input.alias,
		Query:    adapter.Query{Text: text, Scope: input.scope},
		Filtered: len(input.filters) > 0,
		Name:     j.owner,
	}
}

// flatten makes a joined CTE body one relation, whose items carry the
// columns of its SELECT list under their output names.
func (j *joinPlanner) flatten(join *Join) (lowered, error) {
	var names []string
	for _, column := range join.Columns {
		names = append(names, outputName(column))
	}
	if len(names) == 0 || hasDuplicate(names) {
		return lowered{}, unsupported("the joined CTE " + j.owner + " needs a SELECT list with distinct names")
	}
	return lowered{node: &Flatten{Name: j.owner, Input: join}, fields: names}, nil
}

// columnHeader is the column's name in a merged result.
func columnHeader(column JoinColumn) string {
	switch {
	case column.As != "":
		return column.As
	case column.Field == "":
		return column.Alias
	}
	return column.Alias + "." + column.Field
}

// outputName is the column's name in a flattened item.
func outputName(column JoinColumn) string {
	switch {
	case column.As != "":
		return column.As
	case column.Field == "":
		return column.Alias
	}
	return column.Field
}
