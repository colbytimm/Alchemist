package query

import (
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// planner lowers a parsed statement to a Plan: CTEs in the order they are
// declared, so every leaf lands in Plan.Leaves in the order the text names
// it, then the main query.
type planner struct {
	stmt   statement
	leaves []Leaf
	ctes   []*cte // declared so far
	reads  map[string]int
}

// cte is a lowered CTE: the rows its body yields, and what the text says of
// their shape.
type cte struct {
	name   string
	rows   Node
	fields []string
	values bool // SELECT VALUE: its items need not be objects
	shared *Materialize
}

// lowered is a body as a node, with the columns its text names and whether
// it selects VALUE.
type lowered struct {
	node   Node
	fields []string
	values bool
}

func newPlanner(stmt statement) *planner {
	return &planner{stmt: stmt}
}

func (p *planner) plan() (Plan, error) {
	if err := p.checkNames(); err != nil {
		return Plan{}, err
	}
	p.reads = p.countReads()
	for _, text := range p.stmt.ctes {
		body, err := p.lowerBody(text.body, text.name)
		if err != nil {
			return Plan{}, err
		}
		p.ctes = append(p.ctes, &cte{name: text.name, rows: body.node, fields: body.fields, values: body.values})
	}
	main, err := p.lowerBody(p.stmt.main, "")
	if err != nil {
		return Plan{}, err
	}
	return Plan{Leaves: p.leaves, Root: main.node}, nil
}

// checkNames refuses a CTE declared twice, and one named like the first part
// of a dotted source: `sales.orders` beside a CTE `sales` could be a property
// of the CTE or a container, and the planner does not guess.
func (p *planner) checkNames() error {
	names := p.stmt.names()
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			return unsupported("a CTE named " + name + " twice")
		}
	}
	fragments := []fragment{p.stmt.main}
	for _, c := range p.stmt.ctes {
		fragments = append(fragments, c.body)
	}
	for _, f := range fragments {
		for _, s := range parseTokens(f.toks).sources {
			if len(s.path) > 1 && slices.Contains(names, s.path[0].text) {
				return unsupported("a CTE named " + s.path[0].text + " beside the source " + strings.Join(pathText(s.path), "."))
			}
		}
	}
	return nil
}

// countReads counts how often each CTE is read by the main query and by the
// CTEs that are read themselves. A body sees only the CTEs declared before it.
func (p *planner) countReads() map[string]int {
	reads := map[string]int{}
	names := p.stmt.names()
	for _, name := range cteReads(p.stmt.main, names) {
		reads[name]++
	}
	for k := len(p.stmt.ctes) - 1; k >= 0; k-- {
		if reads[names[k]] == 0 {
			continue
		}
		for _, name := range cteReads(p.stmt.ctes[k].body, names[:k]) {
			reads[name]++
		}
	}
	return reads
}

// cteReads lists the one-part sources of f that name one of visible.
func cteReads(f fragment, visible []string) []string {
	var read []string
	for _, s := range parseTokens(f.toks).sources {
		if len(s.path) == 1 && slices.Contains(visible, s.path[0].text) {
			read = append(read, s.path[0].text)
		}
	}
	return read
}

func (p *planner) visible(name string) (*cte, bool) {
	for _, c := range p.ctes {
		if c.name == name {
			return c, true
		}
	}
	return nil, false
}

// rowsOf is where a join reads a CTE: its own node when one reader does,
// and one Materialize all its readers share otherwise.
func (p *planner) rowsOf(c *cte) Node {
	if p.reads[c.name] < 2 {
		return c.rows
	}
	if c.shared == nil {
		c.shared = &Materialize{Name: c.name, Input: c.rows}
	}
	return c.shared
}

// lowerBody lowers one body, the main query's or a CTE's (owner). A body over
// at most one container that reads no CTE and applies no OUTER APPLY is
// opaque: the service runs it whole. A plain container list is a union.
// Anything else is simulated.
func (p *planner) lowerBody(f fragment, owner string) (lowered, error) {
	scanned := parseTokens(f.toks)
	reads, err := p.readsOf(scanned)
	if err != nil {
		return lowered{}, err
	}
	applies := applyKeywords(f.toks)
	if err := checkApplyOperands(f.toks, applies); err != nil {
		return lowered{}, err
	}
	containers := scanned.containerSources()
	if len(reads) == 0 && len(containers) <= 1 && allCrossApplies(f.toks, applies) {
		return p.opaque(f, containers, applies, owner), nil
	}
	if err := checkMergeable(containers); err != nil {
		return lowered{}, err
	}
	if isSourceList(scanned) && len(reads) > 0 {
		return lowered{}, unsupported("a container list of CTEs")
	}
	if len(reads) == 0 && len(applies) == 0 && len(containers) > 1 && !containers[len(containers)-1].joined {
		return p.union(f, scanned.sources, containers, owner)
	}
	return p.simulated(f, owner)
}

// readsOf lists the CTE sources of a body, refusing one read by a subquery.
func (p *planner) readsOf(scanned *parser) ([]source, error) {
	var reads []source
	for _, s := range scanned.sources {
		if len(s.path) != 1 {
			continue
		}
		if _, ok := p.visible(s.path[0].text); !ok {
			continue
		}
		if s.clause != 1 || s.depth != 0 {
			return nil, unsupported("a subquery over the CTE " + s.path[0].text)
		}
		reads = append(reads, s)
	}
	return reads, nil
}

// isSourceList reports whether the first FROM clause lists sources with
// commas.
func isSourceList(scanned *parser) bool {
	listed := 0
	for _, s := range scanned.sources {
		if s.clause == 1 && s.depth == 0 && !s.joined {
			listed++
		}
	}
	return listed > 1
}

func checkMergeable(containers []source) error {
	for _, c := range containers {
		switch {
		case c.clause != 1 && c.depth == 0:
			return unsupported("more than one statement in the editor")
		case c.clause != 1:
			return unsupported("a subquery over another container")
		}
	}
	return nil
}

// applyKeywords indexes the APPLY keywords of toks.
func applyKeywords(toks []token) []int {
	var applies []int
	for i := range toks {
		if keywordAt(toks, i, "APPLY") && !followsDot(toks, i) {
			applies = append(applies, i)
		}
	}
	return applies
}

func checkApplyOperands(toks []token, applies []int) error {
	for _, i := range applies {
		if applyOfQuery(toks, i+1) {
			return unsupported(shapeApplyQuery)
		}
	}
	return nil
}

// allCrossApplies reports whether every APPLY is a CROSS APPLY, which the
// service runs as its own JOIN ... IN.
func allCrossApplies(toks []token, applies []int) bool {
	return !slices.ContainsFunc(applies, func(i int) bool { return !keywordAt(toks, i-1, "CROSS") })
}

// opaque lowers a body the service runs whole: its container is rewritten to
// its alias and its CROSS APPLYs to JOINs, and nothing else changes.
func (p *planner) opaque(f fragment, containers []source, applies []int, owner string) lowered {
	var edits []edit
	for _, i := range applies {
		edits = append(edits, edit{start: f.toks[i-1].start, end: f.toks[i].end, text: "JOIN"})
	}
	leaf := Leaf{Name: owner}
	if len(containers) == 1 {
		leaf.Alias = aliasOrDefault(containers[0].alias)
		leaf.Query.Scope = scopeOf(containers[0])
		edits = append(edits, edit{start: containers[0].start, end: containers[0].end, text: leaf.Alias})
	}
	leaf.Query.Text = f.spliced(edits)
	fields, values := projectionFields(f.toks)
	if owner != "" {
		leaf.Fields = fields
	}
	return lowered{node: &Scan{Leaf: p.addLeaf(leaf)}, fields: fields, values: values}
}

func (p *planner) addLeaf(leaf Leaf) int {
	p.leaves = append(p.leaves, leaf)
	return len(p.leaves) - 1
}

// edit replaces the bytes start to end of the editor text.
type edit struct {
	start, end int
	text       string
}

// spliced is the fragment's text with edits made, which must not overlap.
func (f fragment) spliced(edits []edit) string {
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })
	var out strings.Builder
	at := f.start
	for _, e := range edits {
		out.WriteString(f.text[at:e.start])
		out.WriteString(e.text)
		at = e.end
	}
	out.WriteString(f.text[at:f.end])
	return out.String()
}

func (p *planner) union(f fragment, sources, containers []source, owner string) (lowered, error) {
	if slices.ContainsFunc(containers, func(c source) bool { return c.joined }) {
		return lowered{}, unsupported(shapeListWithJoin)
	}
	if !isPlainList(sources, containers) {
		return lowered{}, unsupported("a container list mixed with other sources")
	}
	alias, err := sharedAlias(containers)
	if err != nil {
		return lowered{}, err
	}
	fields, values := projectionFields(f.toks)
	if fields != nil {
		fields = append([]string{ContainerColumn}, fields...)
	}
	union := &Union{}
	for _, c := range containers {
		leaf := Leaf{
			Alias: alias,
			Query: adapter.Query{Text: rewrittenText(f, containers, alias), Scope: scopeOf(c)},
			Name:  owner,
		}
		union.Leaves = append(union.Leaves, p.addLeaf(leaf))
	}
	return lowered{node: union, fields: fields, values: values}, nil
}

// isPlainList reports whether the first FROM clause holds the containers,
// comma-separated, and nothing else: replacing the list with one alias would
// silently drop anything written in between.
func isPlainList(sources, containers []source) bool {
	listed := 0
	for _, s := range sources {
		if s.clause == 1 {
			listed++
		}
	}
	if listed != len(containers) {
		return false
	}
	for i := 1; i < len(containers); i++ {
		if containers[i-1].nextTok+1 != containers[i].firstTok {
			return false
		}
	}
	return true
}

// sharedAlias is the one alias a container list declares: the body runs
// unchanged against every container, so it can only know them by one name.
func sharedAlias(containers []source) (string, error) {
	alias := ""
	for _, c := range containers {
		switch {
		case c.alias == "" || c.alias == alias:
		case alias == "":
			alias = c.alias
		default:
			return "", unsupported("a container list with more than one alias: write CROSS JOIN")
		}
	}
	return aliasOrDefault(alias), nil
}

func aliasOrDefault(alias string) string {
	if alias == "" {
		return defaultAlias
	}
	return alias
}

// rewrittenText replaces the whole run of container sources with alias.
func rewrittenText(f fragment, containers []source, alias string) string {
	first, last := containers[0], containers[len(containers)-1]
	return f.spliced([]edit{{start: first.start, end: last.end, text: alias}})
}

func scopeOf(s source) []string {
	return []string{s.path[0].text, s.path[1].text}
}

// projectionFields derives the names of the items a SELECT list yields, the
// way the service names them: by AS, by a trailing bare name, or by the last
// segment of a path. Any other item, or *, leaves them unknown (nil). values
// reports SELECT VALUE.
func projectionFields(toks []token) (fields []string, values bool) {
	from := descent{toks: toks}.topLevel(0, "FROM")
	if !keywordAt(toks, 0, "SELECT") || from < 0 {
		return nil, false
	}
	items := toks[1:from]
	if keywordAt(items, 0, "DISTINCT") {
		items = items[1:]
	}
	if keywordAt(items, 0, "TOP") {
		items = items[min(2, len(items)):]
	}
	if keywordAt(items, 0, "VALUE") {
		return nil, true
	}
	for _, item := range splitTopLevel(items) {
		name, ok := itemName(item)
		if !ok {
			return nil, false
		}
		fields = append(fields, name)
	}
	return fields, false
}

// splitTopLevel cuts toks at the commas outside parentheses and brackets.
func splitTopLevel(toks []token) [][]token {
	var cuts []int
	depth := 0
	for i, tok := range toks {
		switch {
		case isSymbol(tok, "("), isSymbol(tok, "["):
			depth++
		case isSymbol(tok, ")"), isSymbol(tok, "]"):
			depth--
		case depth == 0 && tok.kind == tokComma:
			cuts = append(cuts, i)
		}
	}
	return cutAt(toks, cuts)
}

func itemName(item []token) (string, bool) {
	n := len(item)
	switch {
	case n == 0:
		return "", false
	case pathEnd(item, 0) == n:
		return item[n-1].text, true
	case item[n-1].kind != tokIdent || keywords[item[n-1].upper] || n < 2:
		return "", false
	case keywordAt(item, n-2, "AS"), namesAValue(item, n-2):
		return item[n-1].text, true
	}
	return "", false
}

// namesAValue reports whether the token at i closes an operand, so that a
// bare identifier after it names that operand.
func namesAValue(toks []token, i int) bool {
	tok := toks[i]
	switch tok.kind {
	case tokIdent:
		return !keywords[tok.upper]
	case tokNumber, tokString:
		return true
	}
	return isSymbol(tok, ")") || isSymbol(tok, "]")
}
