package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// unionCursor reads its leaves one after another, so a page never mixes
// containers and only one leaf cursor is open at a time.
type unionCursor struct {
	run     *execution
	pending []Leaf
	leaf    Leaf
	current adapter.Cursor
	columns *columnUnion
}

func (r *execution) newUnionCursor(union *Union) *unionCursor {
	leaves := make([]Leaf, 0, len(union.Leaves))
	for _, i := range union.Leaves {
		leaves = append(leaves, r.plan.Leaves[i])
	}
	return &unionCursor{run: r, pending: leaves, columns: newColumnUnion(ContainerColumn)}
}

func (u *unionCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	meter := startMeter()
	page, err := u.readLeafPage(ctx)
	if err != nil {
		return adapter.Page{}, errors.Join(err, u.Close())
	}
	meter.charge(page)
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
	cursor, err := u.run.openLeaf(ctx, u.pending[0])
	if err != nil {
		return err
	}
	u.leaf, u.pending, u.current = u.pending[0], u.pending[1:], cursor
	return nil
}

// tag places the leaf's cells under the merged columns, behind the container
// they came from.
func (u *unionCursor) tag(page adapter.Page) (adapter.Page, error) {
	label := u.leaf.Label()
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
