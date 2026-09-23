package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// joinSide is one container of a join. index is its position in the output:
// 0 for the left side, 1 for the right, whichever of them is hashed.
type joinSide struct {
	index   int
	leaf    Leaf
	key     []string
	columns []JoinColumn // what the SELECT list takes from this side, in its order
	cursor  adapter.Cursor
}

// leafPage is the header of one page of a side. placement says which of its
// cells go where in the merged header, once that is decided.
type leafPage struct {
	side      joinSide
	columns   []string
	placement []cellMove
}

type cellMove struct {
	from int
	to   int
}

func (p leafPage) fill(row, cells []string) {
	for _, move := range p.placement {
		if move.from < len(cells) {
			row[move.to] = cells[move.from]
		}
	}
}

// joinRow is one item of a side, ready to be combined with its matches.
type joinRow struct {
	cells []string
	page  *leafPage
	raw   json.RawMessage
}

// joinCursor is an inner hash join: the build side is read whole into matches
// on the first page, then every page of the probe side is streamed against it.
type joinCursor struct {
	build     joinSide
	probe     joinSide
	aliases   [2]string // left, right
	selectAll bool
	maxRows   int
	columns   *columnUnion
	matches   map[string][]joinRow
	built     bool
	// unplaced are the build pages read before any probe page: the merged
	// header lists the left side first, whichever side was read first.
	unplaced []*leafPage
}

func (e Engine) openJoin(ctx context.Context, plan Plan) (adapter.Cursor, error) {
	left, err := e.Connection.Query(ctx, plan.Leaves[0].Query)
	if err != nil {
		return nil, err
	}
	right, err := e.Connection.Query(ctx, plan.Leaves[1].Query)
	if err != nil {
		return nil, errors.Join(err, left.Close())
	}
	sides := [2]joinSide{
		{index: 0, leaf: plan.Leaves[0], key: plan.Join.LeftKey, cursor: left},
		{index: 1, leaf: plan.Leaves[1], key: plan.Join.RightKey, cursor: right},
	}
	var header []string
	for _, column := range plan.Join.Columns {
		side := &sides[column.Side]
		side.columns = append(side.columns, column)
		header = append(header, columnHeader(side.leaf.Alias, column))
	}
	build := buildSide(plan)
	return &joinCursor{
		build:     sides[build],
		probe:     sides[1-build],
		aliases:   [2]string{plan.Leaves[0].Alias, plan.Leaves[1].Alias},
		selectAll: len(plan.Join.Columns) == 0,
		maxRows:   e.maxJoinRows(),
		columns:   newColumnUnion(header...),
	}, nil
}

// buildSide picks the side to hold in memory. Sizes are unknown before the
// queries run, so a filter is the only evidence of a small side; without one
// to tell them apart it is the joined container, conventionally the lookup.
func buildSide(plan Plan) int {
	if plan.Leaves[0].Filtered && !plan.Leaves[1].Filtered {
		return 0
	}
	return 1
}

func (j *joinCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	meter := startMeter()
	page, err := j.nextMatches(ctx, meter)
	if err != nil {
		return adapter.Page{}, errors.Join(err, j.Close())
	}
	page.Columns = j.columns.snapshot()
	page.Stats = meter.stats(len(page.Rows))
	return page, nil
}

// nextMatches reads probe pages until one of them matches something, so a
// stretch of unmatched rows never surfaces as an empty page with more to come.
func (j *joinCursor) nextMatches(ctx context.Context, meter *meter) (adapter.Page, error) {
	if !j.built {
		if err := j.buildMatches(ctx, meter); err != nil {
			return adapter.Page{}, err
		}
	}
	var page adapter.Page
	for len(page.Rows) == 0 && j.HasMore() {
		rows, err := j.readRows(ctx, &j.probe, meter)
		if err != nil {
			return adapter.Page{}, err
		}
		j.placeLeftFirst()
		for _, probed := range rows {
			j.appendMatches(&page, probed)
		}
	}
	return page, nil
}

func (j *joinCursor) buildMatches(ctx context.Context, meter *meter) error {
	j.matches = map[string][]joinRow{}
	for held := 0; j.build.cursor != nil; {
		rows, err := j.readRows(ctx, &j.build, meter)
		if err != nil {
			return err
		}
		held += len(rows)
		if held > j.maxRows {
			return fmt.Errorf("query: join: %s has more than %d rows to hold in memory: %w",
				j.build.leaf.Label(), j.maxRows, ErrJoinTooLarge)
		}
		for _, row := range rows {
			j.matches[row.key] = append(j.matches[row.key], row.joinRow)
		}
	}
	j.built = true
	if len(j.matches) == 0 {
		return closeSide(&j.probe)
	}
	return nil
}

type keyedRow struct {
	joinRow
	key string
}

// readRows reads the next page of side, closing its cursor after the last
// one. Items without the join key match nothing and are dropped here.
func (j *joinCursor) readRows(ctx context.Context, side *joinSide, meter *meter) ([]keyedRow, error) {
	page, err := side.cursor.NextPage(ctx)
	if err != nil {
		return nil, err
	}
	meter.charge(side.leaf, page)
	if !side.cursor.HasMore() {
		if err := closeSide(side); err != nil {
			return nil, err
		}
	}
	header := &leafPage{side: *side, columns: page.Columns}
	j.unplaced = append(j.unplaced, header)
	var rows []keyedRow
	for i, item := range page.Raw {
		key, ok, err := joinKey(item, side.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		raw, err := j.projectRaw(*side, item)
		if err != nil {
			return nil, err
		}
		row := joinRow{page: header, raw: raw}
		if i < len(page.Rows) {
			row.cells = page.Rows[i]
		}
		rows = append(rows, keyedRow{joinRow: row, key: key})
	}
	return rows, nil
}

func (j *joinCursor) placeLeftFirst() {
	slices.SortStableFunc(j.unplaced, func(a, b *leafPage) int { return a.side.index - b.side.index })
	for _, header := range j.unplaced {
		header.placement = j.place(header.side, header.columns)
	}
	j.unplaced = nil
}

// place maps the columns of one leaf page onto the merged header: all of
// them as alias.column under SELECT *, otherwise the ones the SELECT list
// names, as often as it names them.
func (j *joinCursor) place(side joinSide, columns []string) []cellMove {
	var placement []cellMove
	for from, name := range columns {
		for _, column := range j.selected(side, name) {
			header := columnHeader(side.leaf.Alias, column)
			placement = append(placement, cellMove{from: from, to: j.columns.place([]string{header})[0]})
		}
	}
	return placement
}

func (j *joinCursor) selected(side joinSide, field string) []JoinColumn {
	if j.selectAll {
		return []JoinColumn{{Side: side.index, Field: field}}
	}
	var selected []JoinColumn
	for _, column := range side.columns {
		if column.Field == field {
			selected = append(selected, column)
		}
	}
	return selected
}

func (j *joinCursor) appendMatches(page *adapter.Page, probed keyedRow) {
	for _, held := range j.matches[probed.key] {
		var pair [2]joinRow
		pair[j.build.index], pair[j.probe.index] = held, probed.joinRow
		row := make([]string, len(j.columns.names))
		for _, half := range pair {
			half.page.fill(row, half.cells)
		}
		page.Rows = append(page.Rows, row)
		page.Raw = append(page.Raw, j.pairRaw(pair))
	}
}

// pairRaw nests the two items under their aliases, which are identifiers and
// so quote the same in Go as in JSON.
func (j *joinCursor) pairRaw(pair [2]joinRow) json.RawMessage {
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, half := range pair {
		if i > 0 {
			raw.WriteByte(',')
		}
		raw.WriteString(strconv.Quote(j.aliases[i]))
		raw.WriteByte(':')
		raw.Write(half.raw)
	}
	raw.WriteByte('}')
	return raw.Bytes()
}

// projectRaw cuts an item down to the fields the SELECT list names.
func (j *joinCursor) projectRaw(side joinSide, item json.RawMessage) (json.RawMessage, error) {
	if j.selectAll {
		return item, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(item, &object); err != nil {
		return nil, fmt.Errorf("query: join: read %s item: %w", side.leaf.Label(), err)
	}
	var raw bytes.Buffer
	raw.WriteByte('{')
	for _, column := range side.columns {
		value, ok := object[column.Field]
		if !ok {
			continue
		}
		if raw.Len() > 1 {
			raw.WriteByte(',')
		}
		raw.WriteString(strconv.Quote(rawName(column)))
		raw.WriteByte(':')
		raw.Write(value)
	}
	raw.WriteByte('}')
	return raw.Bytes(), nil
}

func rawName(column JoinColumn) string {
	if column.As != "" {
		return column.As
	}
	return column.Field
}

func (j *joinCursor) HasMore() bool {
	return !j.built || j.probe.cursor != nil
}

func (j *joinCursor) Close() error {
	return errors.Join(closeSide(&j.build), closeSide(&j.probe))
}

func closeSide(side *joinSide) error {
	if side.cursor == nil {
		return nil
	}
	cursor := side.cursor
	side.cursor = nil
	return cursor.Close()
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
	canonical, err := json.Marshal(canonicalNumbers(value))
	if err != nil {
		return "", false, fmt.Errorf("query: join: render key: %w", err)
	}
	return string(canonical), true, nil
}

// canonicalNumbers respells every number so 1, 1.0 and 1e0 are one key, while
// an integer too large for a float64 keeps every digit.
func canonicalNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		return canonicalNumber(v)
	case map[string]any:
		for name, member := range v {
			v[name] = canonicalNumbers(member)
		}
	case []any:
		for i, element := range v {
			v[i] = canonicalNumbers(element)
		}
	}
	return value
}

func canonicalNumber(n json.Number) json.Number {
	if integer, err := n.Int64(); err == nil {
		return json.Number(strconv.FormatInt(integer, 10))
	}
	float, err := n.Float64()
	if err != nil {
		return n
	}
	return json.Number(strconv.FormatFloat(float, 'g', -1, 64))
}
