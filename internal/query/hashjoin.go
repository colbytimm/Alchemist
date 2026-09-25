package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// joinSide is one container of a join, at its index in Plan.Leaves.
type joinSide struct {
	index   int
	leaf    Leaf
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
	keys  []string // parallel to joinCursor.hops; set for the hops that touch this row's side
}

// joinCursor is an inner hash join over a tree of sides. On the first page
// every side but the streamed one is read whole into a hash table, one after
// another; then the streamed side's rows are piped through the tables.
type joinCursor struct {
	connection adapter.Connection
	sides      []joinSide
	streamed   int
	hops       []hop
	tables     []map[string][]joinRow // parallel to hops
	held       int                    // rows across all tables, against maxRows
	unserved   pendingRows
	selectAll  bool
	maxRows    int
	columns    *columnUnion
	built      bool
	closed     bool
	// unplaced are the leaf pages read since the last streamed one: the
	// merged header lists the sides in written order, whichever was read
	// first.
	unplaced []*leafPage
}

func (e Engine) openJoin(plan Plan) *joinCursor {
	sides := make([]joinSide, len(plan.Leaves))
	for i, leaf := range plan.Leaves {
		sides[i] = joinSide{index: i, leaf: leaf}
	}
	var header []string
	for _, column := range plan.Join.Columns {
		side := &sides[column.Side]
		side.columns = append(side.columns, column)
		header = append(header, columnHeader(side.leaf.Alias, column))
	}
	streamed := streamedSide(plan)
	hops := orientSteps(plan, streamed)
	return &joinCursor{
		connection: e.Connection,
		sides:      sides,
		streamed:   streamed,
		hops:       hops,
		tables:     make([]map[string][]joinRow, len(hops)),
		selectAll:  len(plan.Join.Columns) == 0,
		maxRows:    e.maxJoinRows(),
		columns:    newColumnUnion(header...),
	}
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

// build holds every side but the streamed one, then opens the streamed side.
// An empty table ends the run: nothing can match, so nothing more is read.
func (j *joinCursor) build(ctx context.Context, meter *meter) error {
	for h := range j.hops {
		if err := j.hold(ctx, h, meter); err != nil {
			return err
		}
		if len(j.tables[h]) == 0 {
			j.built = true
			return nil
		}
	}
	if err := j.open(ctx, &j.sides[j.streamed]); err != nil {
		return err
	}
	j.built = true
	return nil
}

// hold reads the side hop h leads into to the end, into that hop's table.
func (j *joinCursor) hold(ctx context.Context, h int, meter *meter) error {
	side := &j.sides[j.hops[h].into]
	if err := j.open(ctx, side); err != nil {
		return err
	}
	j.tables[h] = map[string][]joinRow{}
	for side.cursor != nil {
		rows, err := j.readRows(ctx, side, meter)
		if err != nil {
			return err
		}
		j.held += len(rows)
		if j.held > j.maxRows {
			return j.tooLarge(h)
		}
		for _, row := range rows {
			j.tables[h][row.keys[h]] = append(j.tables[h][row.keys[h]], row)
		}
	}
	return nil
}

func (j *joinCursor) open(ctx context.Context, side *joinSide) error {
	cursor, err := j.connection.Query(ctx, side.leaf.Query)
	if err != nil {
		return err
	}
	side.cursor = cursor
	return nil
}

// tooLarge names the side of hop h, which crossed maxRows, and what the
// sides held before it already hold.
func (j *joinCursor) tooLarge(h int) error {
	var before []string
	for earlier := range h {
		before = append(before, fmt.Sprintf("%s %d", j.sides[j.hops[earlier].into].leaf.Label(), rowCount(j.tables[earlier])))
	}
	held := ""
	if len(before) > 0 {
		held = " (" + strings.Join(before, ", ") + ")"
	}
	return fmt.Errorf("query: join: %s takes the rows held in memory past %d%s: %w",
		j.sides[j.hops[h].into].leaf.Label(), j.maxRows, held, ErrJoinTooLarge)
}

func rowCount(table map[string][]joinRow) int {
	count := 0
	for _, rows := range table {
		count += len(rows)
	}
	return count
}

// readRows reads the next page of side, closing its cursor after the last
// one. Items missing a key of any hop their side takes part in match
// nothing and are dropped here.
func (j *joinCursor) readRows(ctx context.Context, side *joinSide, meter *meter) ([]joinRow, error) {
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
	var rows []joinRow
	for i, item := range page.Raw {
		keys, ok, err := j.rowKeys(side.index, item)
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
		row := joinRow{page: header, raw: raw, keys: keys}
		if i < len(page.Rows) {
			row.cells = page.Rows[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// rowKeys renders an item's key for every hop its side takes part in; ok is
// false when any of them is missing.
func (j *joinCursor) rowKeys(side int, item json.RawMessage) (keys []string, ok bool, err error) {
	keys = make([]string, len(j.hops))
	for h, hop := range j.hops {
		path, touches := hop.keyOf(side)
		if !touches {
			continue
		}
		if keys[h], ok, err = joinKey(item, path); !ok {
			return nil, false, err
		}
	}
	return keys, true, nil
}

func (j *joinCursor) placeInWrittenOrder() {
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
	return !j.closed && (!j.built || !j.unserved.empty() || j.sides[j.streamed].cursor != nil)
}

func (j *joinCursor) Close() error {
	j.closed = true
	var errs []error
	for i := range j.sides {
		errs = append(errs, closeSide(&j.sides[i]))
	}
	return errors.Join(errs...)
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
