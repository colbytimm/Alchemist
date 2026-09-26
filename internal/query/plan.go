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

// ContainerColumn is the synthetic leading column of a union's result,
// naming the db.container each row came from.
const ContainerColumn = "_container"

const defaultAlias = "c"

// Plan is an editor query broken into single-container leaves and the tree
// of client-side operators that merges them.
type Plan struct {
	Leaves []Leaf // every single-container query, in the order the text names them
	Root   Node
}

type Leaf struct {
	Alias string
	Query adapter.Query
	// Filtered marks a join input a WHERE or ON condition was pushed down to.
	Filtered bool
	Name     string   // the CTE the leaf is the body of, if any
	Fields   []string // the columns its SELECT list names; nil when the text does not say
}

// Node is one operator of a plan; its cursor serves adapter.Pages.
type Node interface{ isNode() }

// Scan serves one leaf as the adapter returns it.
type Scan struct{ Leaf int }

// Union runs one body against each of its leaves' containers.
type Union struct{ Leaves []int }

// Join combines its Inputs; Steps[i] attaches Inputs[i+1] to an earlier one.
type Join struct {
	Inputs  []Source
	Steps   []JoinStep
	Columns []JoinColumn // empty for SELECT *
	// Absent are the input and APPLY aliases that WHERE NOT IS_DEFINED
	// requires missing from a row.
	Absent []string
}

// Flatten reads a join as one relation of flat items: a joined CTE body.
type Flatten struct {
	Name  string
	Input *Join
}

// Materialize is a CTE read more than once; its readers share the pointer.
type Materialize struct {
	Name  string
	Input Node
}

func (*Scan) isNode()        {}
func (*Union) isNode()       {}
func (*Join) isNode()        {}
func (*Flatten) isNode()     {}
func (*Materialize) isNode() {}

// Source is one input of a join: the rows of a node under an alias, each
// expanded by the APPLYs written after it.
type Source struct {
	Alias   string
	Rows    Node // *Scan, *Union, *Flatten or *Materialize
	Applies []Apply
	Fields  []string // columns known from the text; nil when they are not
}

// Apply ranges Alias over the array at Array; an Outer one keeps an item
// whose array has no element, with Alias absent.
type Apply struct {
	Alias string
	Array FieldRef
	Outer bool
}

type JoinKind int

const (
	InnerJoin JoinKind = iota
	LeftOuterJoin
	RightOuterJoin
	FullOuterJoin
	CrossJoin
)

func (k JoinKind) String() string {
	return [...]string{"INNER", "LEFT", "RIGHT", "FULL", "CROSS"}[k]
}

// JoinStep is one ON equality between the input a JOIN introduced and an
// earlier one.
type JoinStep struct {
	Kind     JoinKind
	Left     int      // index into Join.Inputs of the earlier input
	LeftKey  FieldRef // zero for CrossJoin
	RightKey FieldRef
}

// FieldRef names a field through the alias that binds it: an input's own
// alias or one of its APPLY aliases.
type FieldRef struct {
	Alias string
	Path  []string
}

// JoinColumn is one projected field; Side indexes Join.Inputs.
type JoinColumn struct {
	Side  int
	Alias string
	Field string // empty for an APPLY alias named bare
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
	_, scan := p.Root.(*Scan)
	return p.Root != nil && !scan
}

// Scope is the container the plan's first leaf targets, empty when the query
// named none.
func (p Plan) Scope() []string {
	if len(p.Leaves) == 0 {
		return nil
	}
	return p.Leaves[0].Query.Scope
}

// NeedsDefaultScope reports whether any leaf names no container of its own.
func (p Plan) NeedsDefaultScope() bool {
	return slices.ContainsFunc(p.Leaves, func(leaf Leaf) bool { return len(leaf.Query.Scope) == 0 })
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
// db.container source, and none of the syntax only the engine knows, passes
// through with that source rewritten to its alias; any shape the engine
// cannot prove it simulates correctly is an ErrUnsupported. It never panics
// on arbitrary input.
func BuildPlan(text string) (Plan, error) {
	stmt, err := parseStatement(text)
	if err != nil {
		return Plan{}, err
	}
	return newPlanner(stmt).plan()
}

func unsupported(shape string) error {
	return fmt.Errorf("query: %s: %w", shape, ErrUnsupported)
}
