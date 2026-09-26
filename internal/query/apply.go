package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// expand ranges each APPLY of in over its array, in written order: a row per
// element, none for a row whose array has no element under CROSS APPLY, and
// the row kept with the alias absent under OUTER APPLY.
func (rel *relation) expand(in *joinInput, headers []*leafPage, first joinRow) ([]joinRow, error) {
	rows := []joinRow{first}
	for i, apply := range in.source.Applies {
		slot := i + 1
		var expanded []joinRow
		for _, row := range rows {
			elements, err := arrayAt(row.members[in.slot(apply.Array.Alias)].raw, apply.Array.Path)
			if err != nil {
				return nil, err
			}
			if len(elements) == 0 && apply.Outer {
				expanded = append(expanded, row)
			}
			for _, element := range elements {
				m, err := elementMember(headers[slot], element, rel.namesWhole(in, slot))
				if err != nil {
					return nil, err
				}
				expanded = append(expanded, row.with(slot, m))
			}
		}
		rows = expanded
	}
	return rows, nil
}

func (r joinRow) with(slot int, m member) joinRow {
	r.members = slices.Clone(r.members)
	r.members[slot] = m
	return r
}

// namesWhole reports whether the SELECT list names the APPLY alias of slot
// bare, which renders an object element whole as well as field by field.
func (rel *relation) namesWhole(in *joinInput, slot int) bool {
	alias := in.aliases()[slot]
	return slices.ContainsFunc(in.columns, func(c JoinColumn) bool { return c.Alias == alias && c.Field == "" })
}

// arrayAt is the elements of the array at path within raw: none when raw is
// absent, or the path is missing or holds anything but an array.
func arrayAt(raw json.RawMessage, path []string) ([]json.RawMessage, error) {
	value := raw
	for _, name := range path {
		if !isObject(value) {
			return nil, nil
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, fmt.Errorf("query: apply: read item: %w", err)
		}
		value = object[name]
	}
	if !startsWith(value, '[') {
		return nil, nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(value, &elements); err != nil {
		return nil, fmt.Errorf("query: apply: read array: %w", err)
	}
	return elements, nil
}

func isObject(raw json.RawMessage) bool {
	return startsWith(raw, '{')
}

func startsWith(raw json.RawMessage, c byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == c
}

// elementMember renders an element into the columns of header: an object
// field by field, anything else whole, under the column with no field name.
func elementMember(header *leafPage, element json.RawMessage, whole bool) (member, error) {
	type cell struct {
		at   int
		text string
	}
	var cells []cell
	if isObject(element) {
		names, values, err := objectFields(element)
		if err != nil {
			return member{}, err
		}
		for i, name := range names {
			cells = append(cells, cell{at: header.column(name), text: adapter.RenderCell(values[i])})
		}
	}
	if whole || !isObject(element) {
		cells = append(cells, cell{at: header.column(""), text: adapter.RenderCell(element)})
	}
	rendered := make([]string, len(header.columns))
	for _, c := range cells {
		rendered[c.at] = c.text
	}
	return member{header: header, cells: rendered, raw: element}, nil
}

// objectFields lists an object's fields in the order they are written.
func objectFields(object json.RawMessage) ([]string, []json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(object))
	if _, err := decoder.Token(); err != nil {
		return nil, nil, fmt.Errorf("query: apply: read element: %w", err)
	}
	var names []string
	var values []json.RawMessage
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("query: apply: read element: %w", err)
		}
		name, ok := key.(string)
		if !ok {
			return nil, nil, fmt.Errorf("query: apply: read element: key %v", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, fmt.Errorf("query: apply: read element field %q: %w", name, err)
		}
		names, values = append(names, name), append(values, value)
	}
	return names, values, nil
}
