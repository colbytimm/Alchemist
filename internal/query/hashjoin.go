package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/canonical"
)

// joinCursor is a hash join over a tree of inputs. On the first page every
// input but the streamed one is read whole into a hash table, one after
// another; then the streamed input's rows are piped through the tables, and
// last the held rows an outer step keeps are flushed.
type joinCursor struct {
	run      *execution
	rel      *relation
	streamed int
	hops     []hop
	tables   []*heldTable // parallel to hops
	absent   []aliasSlot
	unserved pendingRows
	// flushed counts the hops done flushing.
	flushed int
	built   bool
	closed  bool
}

func (r *execution) newJoinCursor(join *Join) *joinCursor {
	rel := r.newRelation(join)
	streamed := r.streamedSide(join)
	hops := orientSteps(join, streamed)
	for h, hop := range hops {
		from, into := rel.inputs[hop.from], rel.inputs[hop.into]
		from.keys = append(from.keys, inputKey{hop: h, ref: hop.fromKey, required: !hop.keepFrom})
		into.keys = append(into.keys, inputKey{hop: h, ref: hop.intoKey, required: !hop.keepInto})
	}
	var absent []aliasSlot
	for _, alias := range join.Absent {
		for _, in := range rel.inputs {
			if slot := in.slot(alias); slot >= 0 {
				absent = append(absent, aliasSlot{input: in.index, slot: slot})
			}
		}
	}
	return &joinCursor{
		run:      r,
		rel:      rel,
		streamed: streamed,
		hops:     hops,
		tables:   make([]*heldTable, len(hops)),
		absent:   absent,
	}
}

func (j *joinCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	meter := startMeter()
	page, err := j.nextMatches(ctx, meter)
	if err != nil {
		return adapter.Page{}, errors.Join(err, j.Close())
	}
	page.Columns = j.rel.columns.snapshot()
	page.Stats = meter.stats(len(page.Rows))
	return page, nil
}

// nextMatches serves unserved rows, reading a streamed page or flushing a
// hop only while it has none, so a stretch of unmatched rows never surfaces
// as an empty page with more to come.
func (j *joinCursor) nextMatches(ctx context.Context, meter *meter) (adapter.Page, error) {
	if j.closed {
		return adapter.Page{}, errExhausted
	}
	if !j.built {
		if err := j.build(ctx, meter); err != nil {
			return adapter.Page{}, err
		}
	}
	var page adapter.Page
	for {
		j.serve(&page)
		if len(page.Rows) > 0 {
			return page, nil
		}
		switch {
		case j.streaming():
			rows, err := j.rel.readRows(ctx, j.rel.inputs[j.streamed], len(j.hops), meter)
			if err != nil {
				return adapter.Page{}, err
			}
			for _, row := range rows {
				c := make(combination, len(j.rel.inputs))
				c[j.streamed] = row
				j.unserved.seeds = append(j.unserved.seeds, seed{combination: c})
			}
		case j.flushNext():
		default:
			j.rel.placeInWrittenOrder()
			return page, nil
		}
	}
}

// build holds every input but the streamed one, then opens the streamed
// input. A table nothing can match stops the stream, whose every row would
// be dropped there, and ends the run unless a later hop has rows to flush.
func (j *joinCursor) build(ctx context.Context, meter *meter) error {
	streamDead := false
	for h := range j.hops {
		if err := j.hold(ctx, h, meter); err != nil {
			return err
		}
		if len(j.tables[h].byKey) > 0 || j.hops[h].keepFrom {
			continue
		}
		streamDead = true
		if !j.flushFollows(h) {
			j.flushed = len(j.hops)
			j.built = true
			return nil
		}
	}
	if !streamDead {
		if err := j.openInput(ctx, j.rel.inputs[j.streamed]); err != nil {
			return err
		}
	}
	j.built = true
	return nil
}

// hold reads the input hop h leads into to the end, into that hop's table.
// Its rows count against the run's budget unless a Materialize already
// counted them.
func (j *joinCursor) hold(ctx context.Context, h int, meter *meter) error {
	in := j.rel.inputs[j.hops[h].into]
	if err := j.openInput(ctx, in); err != nil {
		return err
	}
	table := newHeldTable(j.hops[h].keepInto)
	j.tables[h] = table
	holder := -1
	if !in.shared {
		holder = j.run.budget.holder(in.label)
	}
	for in.cursor != nil {
		rows, err := j.rel.readRows(ctx, in, len(j.hops), meter)
		if err != nil {
			return err
		}
		if holder >= 0 {
			if err := j.run.budget.hold(holder, len(rows)); err != nil {
				return err
			}
		}
		for _, row := range rows {
			table.add(row, row.keys[h])
		}
	}
	return nil
}

func (j *joinCursor) openInput(ctx context.Context, in *joinInput) error {
	cursor, err := j.run.open(ctx, in.source.Rows)
	if err != nil {
		return err
	}
	in.cursor = cursor
	return nil
}

func (j *joinCursor) streaming() bool {
	return j.rel.inputs[j.streamed].cursor != nil
}

func (j *joinCursor) HasMore() bool {
	return !j.closed && (!j.built || !j.unserved.empty() || j.streaming() || j.flushPending())
}

func (j *joinCursor) Close() error {
	j.closed = true
	return j.rel.closeInputs()
}

// heldTable is one held input, hashed on its key for one hop. A table whose
// hop keeps its unmatched rows keeps them all, in arrival order, with a flag
// per row.
type heldTable struct {
	rows      []joinRow
	byKey     map[string][]int
	matched   []bool // nil when the hop does not keep unmatched rows
	unmatched int
}

func newHeldTable(keepsUnmatched bool) *heldTable {
	t := &heldTable{byKey: map[string][]int{}}
	if keepsUnmatched {
		t.matched = []bool{}
	}
	return t
}

// add holds row, hashed on key; a row with no key matches nothing, and is
// held only to be flushed.
func (t *heldTable) add(row joinRow, key string) {
	if key != "" {
		t.byKey[key] = append(t.byKey[key], len(t.rows))
	}
	t.rows = append(t.rows, row)
	if t.matched != nil {
		t.matched = append(t.matched, false)
		t.unmatched++
	}
}

// match returns the rows held under key, flagging them matched.
func (t *heldTable) match(key string) []joinRow {
	indexes := t.byKey[key]
	rows := make([]joinRow, 0, len(indexes))
	for _, i := range indexes {
		if t.matched != nil && !t.matched[i] {
			t.matched[i] = true
			t.unmatched--
		}
		rows = append(rows, t.rows[i])
	}
	return rows
}

func (t *heldTable) unmatchedRows() []joinRow {
	var rows []joinRow
	for i, matched := range t.matched {
		if !matched {
			rows = append(rows, t.rows[i])
		}
	}
	return rows
}

// joinKey renders the value at path as canonical JSON, so equal values hash
// alike whatever their spelling. ok is false when the item has no such
// field: undefined equals nothing.
func joinKey(item json.RawMessage, path []string) (key string, ok bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(item))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false, fmt.Errorf("query: join: read key: %w", err)
	}
	for _, name := range path {
		object, isObject := value.(map[string]any)
		if !isObject {
			return "", false, nil
		}
		if value, ok = object[name]; !ok {
			return "", false, nil
		}
	}
	rendered, err := json.Marshal(canonical.Numbers(value))
	if err != nil {
		return "", false, fmt.Errorf("query: join: render key: %w", err)
	}
	return string(rendered), true, nil
}
