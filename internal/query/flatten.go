package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// flattenCursor serves a join's rows as flat items: each column of its
// SELECT list under its output name, as a CTE's readers know them.
type flattenCursor struct {
	join    adapter.Cursor
	columns []JoinColumn
	names   []string
}

func (r *execution) newFlattenCursor(flatten *Flatten) *flattenCursor {
	names := make([]string, 0, len(flatten.Input.Columns))
	for _, column := range flatten.Input.Columns {
		names = append(names, outputName(column))
	}
	return &flattenCursor{join: r.cursor(flatten.Input), columns: flatten.Input.Columns, names: names}
}

func (f *flattenCursor) NextPage(ctx context.Context) (adapter.Page, error) {
	page, err := f.join.NextPage(ctx)
	if err != nil {
		return adapter.Page{}, err
	}
	page.Columns = f.names
	for i, nested := range page.Raw {
		if page.Raw[i], err = f.flatten(nested); err != nil {
			return adapter.Page{}, err
		}
	}
	return page, nil
}

// flatten lifts each column out of the object of its alias; a column absent
// from the nested item is absent from the flat one.
func (f *flattenCursor) flatten(nested json.RawMessage) (json.RawMessage, error) {
	var byAlias map[string]json.RawMessage
	if err := json.Unmarshal(nested, &byAlias); err != nil {
		return nil, fmt.Errorf("query: flatten: read row: %w", err)
	}
	var flat bytes.Buffer
	flat.WriteByte('{')
	for i, column := range f.columns {
		value, ok, err := columnValue(byAlias, column)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if flat.Len() > 1 {
			flat.WriteByte(',')
		}
		flat.WriteString(strconv.Quote(f.names[i]))
		flat.WriteByte(':')
		flat.Write(value)
	}
	flat.WriteByte('}')
	return flat.Bytes(), nil
}

func columnValue(byAlias map[string]json.RawMessage, column JoinColumn) (json.RawMessage, bool, error) {
	value, ok := byAlias[column.Alias]
	if !ok || column.Field == "" {
		return value, ok, nil
	}
	if !isObject(value) {
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(value, &fields); err != nil {
		return nil, false, fmt.Errorf("query: flatten: read %s: %w", column.Alias, err)
	}
	value, ok = fields[rawName(column)]
	return value, ok, nil
}

func (f *flattenCursor) HasMore() bool {
	return f.join.HasMore()
}

func (f *flattenCursor) Close() error {
	return f.join.Close()
}
