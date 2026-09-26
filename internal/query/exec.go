package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

const DefaultMaxJoinRows = 10_000

var (
	// ErrJoinTooLarge aborts a run whose rows in memory outgrew MaxJoinRows.
	ErrJoinTooLarge = errors.New("narrow that side with a WHERE filter, or raise max_join_rows")
	errMalformed    = errors.New("query: execute: plan does not fit its merge step")
	errExhausted    = errors.New("query: no more pages")
)

// Engine executes plans over one connection. Every leaf runs through the
// connection's own cursors, so a merged result costs the sum of its leaves.
type Engine struct {
	Connection adapter.Connection
	// MaxJoinRows caps the rows one run holds in memory: join tables,
	// materialized CTEs, and both sides of a cross join and its product.
	// DefaultMaxJoinRows when zero.
	MaxJoinRows int
}

// Execute returns a cursor over the plan's merged pages without reading
// anything yet. A pass-through plan gets the adapter's cursor itself.
func (e Engine) Execute(ctx context.Context, plan Plan) (adapter.Cursor, error) {
	if err := checkPlan(plan); err != nil {
		return nil, err
	}
	if scan, ok := plan.Root.(*Scan); ok {
		return e.Connection.Query(ctx, plan.Leaves[scan.Leaf].Query)
	}
	run := &execution{
		connection: e.Connection,
		plan:       plan,
		budget:     &rowBudget{max: e.maxJoinRows()},
		shared:     map[*Materialize]*materialized{},
	}
	return run.cursor(plan.Root), nil
}

func (e Engine) maxJoinRows() int {
	if e.MaxJoinRows > 0 {
		return e.MaxJoinRows
	}
	return DefaultMaxJoinRows
}

// execution is one run of a plan: the connection its leaves query, the row
// budget every node of the run draws on, and the CTEs it materializes.
type execution struct {
	connection adapter.Connection
	plan       Plan
	budget     *rowBudget
	shared     map[*Materialize]*materialized
}

// cursor is node's cursor. Nothing is read until its first page.
func (r *execution) cursor(node Node) adapter.Cursor {
	switch n := node.(type) {
	case *Union:
		return r.newUnionCursor(n)
	case *Join:
		if isCross(n) {
			return r.newCrossCursor(n)
		}
		return r.newJoinCursor(n)
	case *Flatten:
		return r.newFlattenCursor(n)
	case *Materialize:
		return r.reader(n)
	}
	return nil
}

// open is node's cursor, ready to read: a leaf's query runs now.
func (r *execution) open(ctx context.Context, node Node) (adapter.Cursor, error) {
	if scan, ok := node.(*Scan); ok {
		return r.openLeaf(ctx, r.plan.Leaves[scan.Leaf])
	}
	return r.cursor(node), nil
}

func (r *execution) openLeaf(ctx context.Context, leaf Leaf) (adapter.Cursor, error) {
	cursor, err := r.connection.Query(ctx, leaf.Query)
	if err != nil {
		return nil, err
	}
	return labeled{Cursor: cursor, label: leaf.chargeLabel()}, nil
}

// label is what the row budget calls the rows of node.
func (r *execution) label(node Node) string {
	switch n := node.(type) {
	case *Scan:
		return r.plan.Leaves[n.Leaf].chargeLabel()
	case *Union:
		return r.plan.Leaves[n.Leaves[0]].Name
	case *Flatten:
		return n.Name
	case *Materialize:
		return n.Name
	}
	return ""
}

// labeled files the charge of each page of a leaf under the leaf's label.
type labeled struct {
	adapter.Cursor
	label string
}

func (l labeled) NextPage(ctx context.Context) (adapter.Page, error) {
	page, err := l.Cursor.NextPage(ctx)
	if err != nil {
		return page, err
	}
	page.Stats.LeafCharges = map[string]float64{l.label: page.Stats.RequestCharge}
	return page, nil
}

// Label is the db.container name a union tags the leaf's rows with.
func (l Leaf) Label() string {
	return strings.Join(l.Query.Scope, ".")
}

// chargeLabel is what the leaf's charges are filed under: its container,
// behind the name of the CTE it is the body of.
func (l Leaf) chargeLabel() string {
	if l.Name == "" {
		return l.Label()
	}
	return l.Name + " (" + l.Label() + ")"
}

// rowBudget is what one run may hold in memory, shared by every node that
// holds rows.
type rowBudget struct {
	max  int
	held []heldRows
}

type heldRows struct {
	label string
	rows  int
}

// holder registers a holder of rows and returns its index.
func (b *rowBudget) holder(label string) int {
	b.held = append(b.held, heldRows{label: label})
	return len(b.held) - 1
}

// hold adds rows to what holder h holds; past the cap, it names h and what
// the holders before it already hold.
func (b *rowBudget) hold(h, rows int) error {
	b.held[h].rows += rows
	if b.total() <= b.max {
		return nil
	}
	var before []string
	for _, earlier := range b.held[:h] {
		before = append(before, fmt.Sprintf("%s %d", earlier.label, earlier.rows))
	}
	held := ""
	if len(before) > 0 {
		held = " (" + strings.Join(before, ", ") + ")"
	}
	return fmt.Errorf("query: join: %s takes the rows held in memory past %d%s: %w",
		b.held[h].label, b.max, held, ErrJoinTooLarge)
}

func (b *rowBudget) total() int {
	total := 0
	for _, h := range b.held {
		total += h.rows
	}
	return total
}

// meter totals what one merged page cost across the pages behind it.
type meter struct {
	start   time.Time
	total   float64
	perLeaf map[string]float64
}

func startMeter() *meter {
	return &meter{start: time.Now(), perLeaf: map[string]float64{}}
}

// charge adds a page read from a child: a leaf's, labeled, or a nested
// node's, already broken down by leaf.
func (m *meter) charge(page adapter.Page) {
	m.total += page.Stats.RequestCharge
	for label, charge := range page.Stats.LeafCharges {
		m.perLeaf[label] += charge
	}
}

func (m *meter) stats(rows int) adapter.Stats {
	return adapter.Stats{
		RequestCharge: m.total,
		Elapsed:       time.Since(m.start),
		RowCount:      rows,
		LeafCharges:   m.perLeaf,
	}
}
