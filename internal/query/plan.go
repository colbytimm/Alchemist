package query

import (
	"errors"
	"fmt"
	"slices"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// ErrUnsupported marks a cross-container query the planner cannot prove it
// simulates correctly.
var ErrUnsupported = errors.New("not supported across containers: run it server-side, one container at a time")

// ContainerColumn is the synthetic leading column of a UnionAll result,
// naming the db.container each row came from.
const ContainerColumn = "_container"

const defaultAlias = "c"

// Merge is how the pages of a plan's leaves become one result set.
type Merge int

const (
	PassThrough Merge = iota // one leaf, served as the adapter returns it
	UnionAll
	HashJoin
)

// Plan is an editor query broken into single-container leaves plus the
// client-side step that merges them.
type Plan struct {
	Merge  Merge
	Leaves []Leaf
	Join   Join // set for HashJoin, whose Leaves are its left and right side
}

type Leaf struct {
	Alias string
	Query adapter.Query
	// Filtered marks a join side a WHERE condition was pushed down to.
	Filtered bool
}

type Join struct {
	LeftKey  []string // field path within a left-side item
	RightKey []string
	Columns  []JoinColumn // empty for SELECT *
}

// JoinColumn is one projected field; Side indexes Plan.Leaves.
type JoinColumn struct {
	Side  int
	Field string
	As    string // the name `AS` gave it, if any
}

// WholeItems reports whether the leaf's rows are items as stored, which a
// SELECT list other than * projects into something else.
func (l Leaf) WholeItems() bool {
	toks := code(lex(l.Query.Text))
	i := 1
	if keywordAt(toks, i, "TOP") {
		i += 2
	}
	return keywordAt(toks, 0, "SELECT") && i < len(toks) && isSymbol(toks[i], "*") && keywordAt(toks, i+1, "FROM")
}

// Simulated reports whether the plan is merged client-side rather than run
// as-is by the service.
func (p Plan) Simulated() bool {
	return p.Merge != PassThrough
}

// Scope is the container the plan's first leaf targets, empty when the query
// named none.
func (p Plan) Scope() []string {
	if len(p.Leaves) == 0 {
		return nil
	}
	return p.Leaves[0].Query.Scope
}

// WithDefaultScope targets every leaf whose query named no container at scope.
func (p Plan) WithDefaultScope(scope []string) Plan {
	p.Leaves = slices.Clone(p.Leaves)
	for i := range p.Leaves {
		if len(p.Leaves[i].Query.Scope) == 0 {
			p.Leaves[i].Query.Scope = scope
		}
	}
	return p
}

// BuildPlan plans text without executing it. A query over at most one
// db.container source passes through with that source rewritten to its alias;
// any shape over several that cannot be simulated is an ErrUnsupported. It
// never panics on arbitrary input.
func BuildPlan(text string) (Plan, error) {
	p := parse(text)
	containers := p.containerSources()
	switch len(containers) {
	case 0:
		return passThrough(Leaf{Query: adapter.Query{Text: text}}), nil
	case 1:
		return passThrough(rewrittenLeaf(text, containers, aliasOrDefault(containers[0].alias))), nil
	}
	if err := checkMergeable(containers); err != nil {
		return Plan{}, err
	}
	if containers[len(containers)-1].joined {
		return planJoin(text, p.toks, containers)
	}
	return planUnion(text, p.sources, containers)
}

// containerSources are the sources of the form db.container; a two-part path
// rooted at an alias is a property of that alias instead.
func (p *parser) containerSources() []source {
	var containers []source
	for _, s := range p.sources {
		if len(s.path) == 2 && !p.aliases[s.path[0].text] {
			containers = append(containers, s)
		}
	}
	return containers
}

func checkMergeable(containers []source) error {
	for _, c := range containers {
		switch {
		case c.clause != 1 && c.depth == 0:
			return unsupported("more than one statement in the editor")
		case c.clause != 1:
			return unsupported("a subquery over another container")
		case c.modifier != "" && c.modifier != "INNER":
			return unsupported(c.modifier + " JOIN")
		}
	}
	return nil
}

func planUnion(text string, sources, containers []source) (Plan, error) {
	if slices.ContainsFunc(containers, func(c source) bool { return c.joined }) {
		return Plan{}, unsupported("a join over more than two containers")
	}
	if !isPlainList(sources, containers) {
		return Plan{}, unsupported("a container list mixed with other sources")
	}
	alias, err := sharedAlias(containers)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Merge: UnionAll}
	for _, c := range containers {
		leaf := rewrittenLeaf(text, containers, alias)
		leaf.Query.Scope = scopeOf(c)
		plan.Leaves = append(plan.Leaves, leaf)
	}
	return plan, nil
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
			return "", unsupported("a container list with more than one alias")
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

// rewrittenLeaf replaces the whole run of container sources with alias and
// targets the first of them.
func rewrittenLeaf(text string, containers []source, alias string) Leaf {
	first, last := containers[0], containers[len(containers)-1]
	return Leaf{
		Alias: alias,
		Query: adapter.Query{Text: text[:first.start] + alias + text[last.end:], Scope: scopeOf(first)},
	}
}

func passThrough(leaf Leaf) Plan {
	return Plan{Merge: PassThrough, Leaves: []Leaf{leaf}}
}

func scopeOf(s source) []string {
	return []string{s.path[0].text, s.path[1].text}
}

func unsupported(shape string) error {
	return fmt.Errorf("query: %s: %w", shape, ErrUnsupported)
}
