package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

const DefaultMaxJoinRows = 10_000

var (
	// ErrJoinTooLarge aborts a join whose in-memory side outgrew MaxJoinRows.
	ErrJoinTooLarge = errors.New("narrow that side with a WHERE filter, or raise max_join_rows")
	errMalformed    = errors.New("query: execute: plan does not fit its merge step")
	errExhausted    = errors.New("query: no more pages")
)

// Engine executes plans over one connection. Every leaf runs through the
// connection's own cursors, so a merged result costs the sum of its leaves.
type Engine struct {
	Connection  adapter.Connection
	MaxJoinRows int // DefaultMaxJoinRows when zero
}

// Execute returns a cursor over the plan's merged pages. A pass-through plan
// gets the adapter's cursor itself.
func (e Engine) Execute(ctx context.Context, plan Plan) (adapter.Cursor, error) {
	switch {
	case plan.Merge == PassThrough && len(plan.Leaves) == 1:
		return e.Connection.Query(ctx, plan.Leaves[0].Query)
	case plan.Merge == UnionAll && len(plan.Leaves) > 0:
		return newUnionCursor(e.Connection, plan.Leaves), nil
	case plan.Merge == HashJoin && len(plan.Leaves) == 2:
		return e.openJoin(ctx, plan)
	}
	return nil, errMalformed
}

func (e Engine) maxJoinRows() int {
	if e.MaxJoinRows > 0 {
		return e.MaxJoinRows
	}
	return DefaultMaxJoinRows
}

// unionCursor reads its leaves one after another, so a page never mixes
// containers and only one leaf cursor is open at a time.
type unionCursor struct {
	connection adapter.Connection
	pending    []Leaf
	leaf       Leaf
	current    adapter.Cursor
	columns    *columnUnion
}

func newUnionCursor(connection adapter.Connection, leaves []Leaf) *unionCursor {
	return &unionCursor{connection: connection, pending: leaves, columns: newColumnUnion(ContainerColumn)}
}

func (u *unionCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	meter := startMeter()
	page, err := u.readLeafPage(ctx)
	if err != nil {
		return adapter.Page{}, errors.Join(err, u.Close())
	}
	meter.charge(u.leaf, page)
	merged, err := u.tag(page)
	if err != nil {
		return adapter.Page{}, errors.Join(err, u.Close())
	}
	merged.Stats = meter.stats(len(merged.Rows))
	return merged, nil
}

func (u *unionCursor) readLeafPage(ctx context.Context) (adapter.Page, error) {
	if u.current == nil {
		if err := u.openNextLeaf(ctx); err != nil {
			return adapter.Page{}, err
		}
	}
	page, err := u.current.NextPage(ctx)
	if err != nil || u.current.HasMore() {
		return page, err
	}
	return page, u.Close()
}

func (u *unionCursor) openNextLeaf(ctx context.Context) error {
	if len(u.pending) == 0 {
		return errExhausted
	}
	cursor, err := u.connection.Query(ctx, u.pending[0].Query)
	if err != nil {
		return err
	}
	u.leaf, u.pending, u.current = u.pending[0], u.pending[1:], cursor
	return nil
}

// tag places the leaf's cells under the merged columns, behind the container
// they came from.
func (u *unionCursor) tag(page adapter.Page) (adapter.Page, error) {
	label := leafLabel(u.leaf)
	field, err := containerField(label)
	if err != nil {
		return adapter.Page{}, err
	}
	placement := u.columns.place(page.Columns)
	merged := adapter.Page{Columns: u.columns.snapshot()}
	for _, cells := range page.Rows {
		row := make([]string, len(merged.Columns))
		placeCells(row, cells, placement)
		row[0] = label
		merged.Rows = append(merged.Rows, row)
	}
	for _, raw := range page.Raw {
		merged.Raw = append(merged.Raw, tagRaw(raw, field))
	}
	return merged, nil
}

func placeCells(row, cells []string, placement []int) {
	for c, at := range placement {
		if c < len(cells) {
			row[at] = cells[c]
		}
	}
}

func (u *unionCursor) HasMore() bool {
	return u.current != nil || len(u.pending) > 0
}

func (u *unionCursor) Close() error {
	if u.current == nil {
		return nil
	}
	cursor := u.current
	u.current = nil
	return cursor.Close()
}

// containerField is an object opened on its container field: `{"_container":"db.c"`.
func containerField(label string) ([]byte, error) {
	object, err := json.Marshal(map[string]string{ContainerColumn: label})
	if err != nil {
		return nil, fmt.Errorf("query: union: tag %q: %w", label, err)
	}
	return bytes.TrimSuffix(object, []byte("}")), nil
}

// tagRaw puts field at the head of an item. Anything but a JSON object, the
// product of SELECT VALUE, has no place for it and is left as it is.
func tagRaw(raw json.RawMessage, field []byte) json.RawMessage {
	body := bytes.TrimSpace(raw)
	if len(body) < 2 || body[0] != '{' {
		return raw
	}
	tagged := bytes.Clone(field)
	fields := bytes.TrimSpace(body[1:])
	if fields[0] != '}' {
		tagged = append(tagged, ',')
	}
	return append(tagged, fields...)
}

func leafLabel(leaf Leaf) string {
	return strings.Join(leaf.Query.Scope, ".")
}

// columnUnion is the header of a merged result. Like an adapter's, it only
// ever grows at the end, so a later page never moves a column already on
// screen.
type columnUnion struct {
	names []string
	index map[string]int
}

func newColumnUnion(names ...string) *columnUnion {
	c := &columnUnion{index: map[string]int{}}
	c.place(names)
	return c
}

// place returns where each of names sits in the header, adding the new ones.
func (c *columnUnion) place(names []string) []int {
	placement := make([]int, len(names))
	for i, name := range names {
		at, ok := c.index[name]
		if !ok {
			at = len(c.names)
			c.index[name] = at
			c.names = append(c.names, name)
		}
		placement[i] = at
	}
	return placement
}

func (c *columnUnion) snapshot() []string {
	return slices.Clone(c.names)
}

// meter totals what one merged page cost across the leaf pages behind it.
type meter struct {
	start   time.Time
	total   float64
	perLeaf map[string]float64
}

func startMeter() *meter {
	return &meter{start: time.Now(), perLeaf: map[string]float64{}}
}

func (m *meter) charge(leaf Leaf, page adapter.Page) {
	m.total += page.Stats.RequestCharge
	m.perLeaf[leafLabel(leaf)] += page.Stats.RequestCharge
}

func (m *meter) stats(rows int) adapter.Stats {
	return adapter.Stats{
		RequestCharge: m.total,
		Elapsed:       time.Since(m.start),
		RowCount:      rows,
		LeafCharges:   m.perLeaf,
	}
}
