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

// joinInput is one input of a join, at its index in Join.Inputs.
type joinInput struct {
	index  int
	source Source
	label  string
	cursor adapter.Cursor
	// columns are what the SELECT list takes from this input, in its order.
	columns []JoinColumn
	keys    []inputKey
	// shared marks an input whose items a Materialize holds, and counted.
	shared      bool
	knownPlaced bool
}

// inputKey is a join key read from the rows of an input for one hop.
type inputKey struct {
	hop int
	ref FieldRef
	// required drops a row without the key as it is read: no result can
	// hold it.
	required bool
}

// aliases are the names the input binds, one per slot of its rows: its own,
// then its APPLYs'.
func (in *joinInput) aliases() []string {
	names := []string{in.source.Alias}
	for _, apply := range in.source.Applies {
		names = append(names, apply.Alias)
	}
	return names
}

// heldRows is what holding rows adds to the budget: all of them, or for a
// Materialize's items only the rows APPLYs added beyond one per item.
func (in *joinInput) heldRows(rows, expansion int) int {
	if !in.shared {
		return rows
	}
	return expansion
}

func (in *joinInput) slot(alias string) int {
	return slices.Index(in.aliases(), alias)
}

func (in *joinInput) close() error {
	if in.cursor == nil {
		return nil
	}
	cursor := in.cursor
	in.cursor = nil
	return cursor.Close()
}

// joinRow is one row of an input after its APPLYs: the item, then the
// element of each APPLY, each in the slot of its alias. The zero joinRow is
// an input absent from a combination.
type joinRow struct {
	members []member
	// keys is parallel to the hops: the rendered key this row takes to each,
	// or "" where it takes none. No rendered key is empty.
	keys []string
}

func (r joinRow) present() bool {
	return len(r.members) > 0
}

// member is an item or an element of a row; a nil raw is an APPLY alias
// absent from it.
type member struct {
	header *leafPage
	cells  []string
	raw    json.RawMessage
}

// leafPage is the header of one page of an input's slot. placement says
// which of its cells go where in the merged header, once that is decided.
type leafPage struct {
	input     *joinInput
	slot      int
	columns   []string
	index     map[string]int
	placement []cellMove
}

type cellMove struct {
	from int
	to   int
}

func (p *leafPage) fill(row, cells []string) {
	for _, move := range p.placement {
		if move.from < len(cells) {
			row[move.to] = cells[move.from]
		}
	}
}

// column is where name sits among the page's columns, adding it if new.
func (p *leafPage) column(name string) int {
	if at, ok := p.index[name]; ok {
		return at
	}
	p.index[name] = len(p.columns)
	p.columns = append(p.columns, name)
	return len(p.columns) - 1
}

// combination is one candidate merged row: a joinRow per input, indexed as
// Join.Inputs.
type combination []joinRow

// relation reads a join's inputs into rows and renders combinations of them
// as merged rows.
type relation struct {
	inputs    []*joinInput
	columns   *columnUnion
	selectAll bool
	// absent are the aliases WHERE NOT IS_DEFINED wants missing from a row.
	absent []aliasSlot
	// unplaced are the pages read since the last placement: the merged
	// header lists the inputs in written order, whichever was read first.
	unplaced []*leafPage
}

func (r *execution) newRelation(join *Join) *relation {
	rel := &relation{selectAll: len(join.Columns) == 0}
	for i, source := range join.Inputs {
		_, shared := source.Rows.(*Materialize)
		rel.inputs = append(rel.inputs, &joinInput{index: i, source: source, label: r.label(source.Rows), shared: shared})
	}
	var header []string
	for _, column := range join.Columns {
		input := rel.inputs[column.Side]
		input.columns = append(input.columns, column)
		header = append(header, columnHeader(column))
	}
	rel.columns = newColumnUnion(header...)
	for _, alias := range join.Absent {
		for _, in := range rel.inputs {
			if slot := in.slot(alias); slot >= 0 {
				rel.absent = append(rel.absent, aliasSlot{input: in.index, slot: slot})
			}
		}
	}
	return rel
}

// readRows reads the next page of in, closing its cursor after the last one,
// and returns its items as rows: expanded by the input's APPLYs, keyed for
// the hops, and cut down to what the SELECT list names. expansion counts the
// rows beyond the first that each item became.
func (rel *relation) readRows(ctx context.Context, in *joinInput, hops int, meter *meter) (rows []joinRow, expansion int, err error) {
	page, err := in.cursor.NextPage(ctx)
	if err != nil {
		return nil, 0, err
	}
	meter.charge(page)
	if !in.cursor.HasMore() {
		if err := in.close(); err != nil {
			return nil, 0, err
		}
	}
	headers := rel.headers(in, page.Columns)
	for i, item := range page.Raw {
		first := joinRow{members: make([]member, len(headers))}
		first.members[0] = member{header: headers[0], raw: item}
		if i < len(page.Rows) {
			first.members[0].cells = page.Rows[i]
		}
		expanded, err := rel.expand(in, headers, first)
		if err != nil {
			return nil, 0, err
		}
		kept := 0
		for _, row := range expanded {
			finished, err := rel.finish(in, row, hops)
			if err != nil {
				return nil, 0, err
			}
			if finished.present() {
				rows = append(rows, finished)
				kept++
			}
		}
		expansion += max(kept-1, 0)
	}
	return rows, expansion, nil
}

// headers opens one leafPage per slot of in for a page of its items.
func (rel *relation) headers(in *joinInput, columns []string) []*leafPage {
	headers := []*leafPage{{input: in, columns: columns}}
	for slot := 1; slot <= len(in.source.Applies); slot++ {
		headers = append(headers, &leafPage{input: in, slot: slot, index: map[string]int{}})
	}
	rel.unplaced = append(rel.unplaced, headers...)
	return headers
}

// finish keys a row for the hops and projects its members, or drops it: the
// zero joinRow is a row missing a key it cannot do without.
func (rel *relation) finish(in *joinInput, row joinRow, hops int) (joinRow, error) {
	if hops > 0 {
		row.keys = make([]string, hops)
	}
	for _, key := range in.keys {
		member := row.members[in.slot(key.ref.Alias)]
		rendered, ok := "", false
		if member.raw != nil {
			var err error
			if rendered, ok, err = joinKey(member.raw, key.ref.Path); err != nil {
				return joinRow{}, err
			}
		}
		if !ok && key.required {
			return joinRow{}, nil
		}
		row.keys[key.hop] = rendered
	}
	for slot, member := range row.members {
		if member.raw == nil {
			continue
		}
		projected, err := rel.project(in, slot, member.raw)
		if err != nil {
			return joinRow{}, err
		}
		row.members[slot].raw = projected
	}
	return row, nil
}

// project cuts an item or element down to the fields the SELECT list names
// through its alias; an element named bare is kept whole.
func (rel *relation) project(in *joinInput, slot int, raw json.RawMessage) (json.RawMessage, error) {
	if rel.selectAll {
		return raw, nil
	}
	alias := in.aliases()[slot]
	var named []JoinColumn
	for _, column := range in.columns {
		if column.Alias != alias {
			continue
		}
		if column.Field == "" {
			return raw, nil
		}
		named = append(named, column)
	}
	object := map[string]json.RawMessage{}
	if isObject(raw) {
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, fmt.Errorf("query: join: read %s item: %w", in.label, err)
		}
	}
	var projected bytes.Buffer
	projected.WriteByte('{')
	for _, column := range named {
		value, ok := object[column.Field]
		if !ok {
			continue
		}
		if projected.Len() > 1 {
			projected.WriteByte(',')
		}
		projected.WriteString(strconv.Quote(rawName(column)))
		projected.WriteByte(':')
		projected.Write(value)
	}
	projected.WriteByte('}')
	return projected.Bytes(), nil
}

func rawName(column JoinColumn) string {
	if column.As != "" {
		return column.As
	}
	return column.Field
}

func (rel *relation) placeInWrittenOrder() {
	slices.SortStableFunc(rel.unplaced, func(a, b *leafPage) int {
		if a.input.index != b.input.index {
			return a.input.index - b.input.index
		}
		return a.slot - b.slot
	})
	for _, in := range rel.inputs {
		rel.placeKnownFields(in)
		for _, header := range rel.unplaced {
			if header.input == in {
				header.placement = rel.place(header)
			}
		}
	}
	rel.unplaced = nil
}

// placeKnownFields lists the columns a CTE's SELECT list names, so its
// columns are on screen whether or not it returns anything.
func (rel *relation) placeKnownFields(in *joinInput) {
	if !rel.selectAll || in.knownPlaced || in.source.Fields == nil {
		return
	}
	in.knownPlaced = true
	for _, field := range in.source.Fields {
		rel.columns.place([]string{starHeader(in.source.Alias, field)})
	}
}

// place maps the columns of one page onto the merged header: all of them
// under SELECT *, otherwise the ones the SELECT list names, as often as it
// names them.
func (rel *relation) place(header *leafPage) []cellMove {
	var placement []cellMove
	for from, name := range header.columns {
		for _, column := range rel.selected(header, name) {
			placement = append(placement, cellMove{from: from, to: rel.columns.place([]string{columnHeader(column)})[0]})
		}
	}
	return placement
}

func (rel *relation) selected(header *leafPage, field string) []JoinColumn {
	alias := header.input.aliases()[header.slot]
	if rel.selectAll {
		return []JoinColumn{{Side: header.input.index, Alias: alias, Field: field}}
	}
	var selected []JoinColumn
	for _, column := range header.input.columns {
		if column.Alias == alias && column.Field == field {
			selected = append(selected, column)
		}
	}
	return selected
}

// starHeader is the SELECT * name of a field of alias; a scalar element has
// no field name and is named by its alias.
func starHeader(alias, field string) string {
	return columnHeader(JoinColumn{Alias: alias, Field: field})
}

func (rel *relation) appendRow(page *adapter.Page, c combination) {
	row := make([]string, len(rel.columns.names))
	for _, input := range c {
		for _, member := range input.members {
			if member.raw != nil {
				member.header.fill(row, member.cells)
			}
		}
	}
	page.Rows = append(page.Rows, row)
	page.Raw = append(page.Raw, rel.combinedRaw(c))
}

// combinedRaw nests every item and element under its alias in written
// order, leaving out what is absent. Aliases are identifiers, and so quote
// the same in Go as in JSON.
func (rel *relation) combinedRaw(c combination) json.RawMessage {
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, input := range c {
		aliases := rel.inputs[i].aliases()
		for slot, member := range input.members {
			if member.raw == nil {
				continue
			}
			if raw.Len() > 1 {
				raw.WriteByte(',')
			}
			raw.WriteString(strconv.Quote(aliases[slot]))
			raw.WriteByte(':')
			raw.Write(member.raw)
		}
	}
	raw.WriteByte('}')
	return raw.Bytes()
}

func (rel *relation) closeInputs() error {
	var errs []error
	for _, in := range rel.inputs {
		errs = append(errs, in.close())
	}
	return errors.Join(errs...)
}
