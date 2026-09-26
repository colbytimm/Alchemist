package query

import (
	"context"
	"errors"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// materialized is a CTE's body read to the end, once per run.
type materialized struct {
	pages []adapter.Page
}

// materializedReader is one reader of a Materialize. The first reader to ask
// for a page runs the body into memory, and pays for it on that page; every
// reader is then served from memory.
type materializedReader struct {
	run    *execution
	node   *Materialize
	next   int
	closed bool
}

func (r *execution) reader(node *Materialize) *materializedReader {
	return &materializedReader{run: r, node: node}
}

func (m *materializedReader) NextPage(ctx context.Context) (adapter.Page, error) {
	if m.closed {
		return adapter.Page{}, errExhausted
	}
	meter := startMeter()
	shared, ok := m.run.shared[m.node]
	if !ok {
		var err error
		if shared, err = m.run.materialize(ctx, m.node, meter); err != nil {
			return adapter.Page{}, err
		}
		m.run.shared[m.node] = shared
	}
	if m.next >= len(shared.pages) {
		return adapter.Page{}, errExhausted
	}
	page := shared.pages[m.next]
	m.next++
	page.Stats = meter.stats(len(page.Rows))
	return page, nil
}

// materialize reads node's body to the end, counting its rows against the
// run's budget once for all its readers.
func (r *execution) materialize(ctx context.Context, node *Materialize, meter *meter) (*materialized, error) {
	cursor, err := r.open(ctx, node.Input)
	if err != nil {
		return nil, err
	}
	holder := r.budget.holder(node.Name)
	shared := &materialized{}
	for first := true; first || cursor.HasMore(); first = false {
		page, err := cursor.NextPage(ctx)
		if err != nil {
			return nil, errors.Join(err, cursor.Close())
		}
		meter.charge(page)
		if err := r.budget.hold(holder, len(page.Raw)); err != nil {
			return nil, errors.Join(err, cursor.Close())
		}
		page.Stats = adapter.Stats{}
		shared.pages = append(shared.pages, page)
	}
	return shared, cursor.Close()
}

func (m *materializedReader) HasMore() bool {
	if m.closed {
		return false
	}
	shared, ok := m.run.shared[m.node]
	return !ok || m.next < len(shared.pages)
}

func (m *materializedReader) Close() error {
	m.closed = true
	return nil
}
