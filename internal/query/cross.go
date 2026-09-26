package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// crossCursor pairs every row of its first input with every row of its
// second. Both are read whole, and the product sized, before the first page;
// the pages are then served from memory, first-input row major.
type crossCursor struct {
	run    *execution
	rel    *relation
	sides  [2][]joinRow
	next   int // index into the product of the next row to serve
	built  bool
	closed bool
}

func isCross(join *Join) bool {
	return len(join.Steps) == 1 && join.Steps[0].Kind == CrossJoin
}

func (r *execution) newCrossCursor(join *Join) *crossCursor {
	return &crossCursor{run: r, rel: r.newRelation(join)}
}

func (c *crossCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	if c.closed {
		return adapter.Page{}, errExhausted
	}
	meter := startMeter()
	if !c.built {
		if err := c.build(ctx, meter); err != nil {
			return adapter.Page{}, errors.Join(err, c.Close())
		}
	}
	c.rel.placeInWrittenOrder()
	var page adapter.Page
	for len(page.Rows) < maxMergedPageRows && c.next < c.size() {
		right := len(c.sides[1])
		pair := combination{c.sides[0][c.next/right], c.sides[1][c.next%right]}
		if !c.rel.hasPresentAbsent(pair) {
			c.rel.appendRow(&page, pair)
		}
		c.next++
	}
	page.Columns = c.rel.columns.snapshot()
	page.Stats = meter.stats(len(page.Rows))
	return page, nil
}

// build holds both inputs, the second only when the first has rows, and
// refuses a product past the cap before anything is served.
func (c *crossCursor) build(ctx context.Context, meter *meter) error {
	for i, in := range c.rel.inputs {
		if i > 0 && len(c.sides[0]) == 0 {
			break
		}
		rows, err := c.holdWhole(ctx, in, meter)
		if err != nil {
			return err
		}
		c.sides[i] = rows
	}
	if c.size() > c.run.budget.max {
		first, second := c.rel.inputs[0], c.rel.inputs[1]
		return fmt.Errorf("query: join: cross join of %s (%d rows) and %s (%d rows) is %d rows, past %d: %w",
			first.label, len(c.sides[0]), second.label, len(c.sides[1]), c.size(), c.run.budget.max, ErrJoinTooLarge)
	}
	c.built = true
	return nil
}

func (c *crossCursor) holdWhole(ctx context.Context, in *joinInput, meter *meter) ([]joinRow, error) {
	cursor, err := c.run.open(ctx, in.source.Rows)
	if err != nil {
		return nil, err
	}
	in.cursor = cursor
	holder := c.run.budget.holder(in.label)
	var held []joinRow
	for in.cursor != nil {
		rows, expansion, err := c.rel.readRows(ctx, in, 0, meter)
		if err != nil {
			return nil, err
		}
		if err := c.run.budget.hold(holder, in.budgetedRows(len(rows), expansion)); err != nil {
			return nil, err
		}
		held = append(held, rows...)
	}
	return held, nil
}

func (c *crossCursor) size() int {
	return len(c.sides[0]) * len(c.sides[1])
}

func (c *crossCursor) HasMore() bool {
	return !c.closed && (!c.built || c.next < c.size())
}

func (c *crossCursor) Close() error {
	c.closed = true
	return c.rel.closeInputs()
}
