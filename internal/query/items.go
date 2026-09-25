package query

import (
	"bytes"
	"encoding/json"
)

// LeafItems sorts a page's items back to the leaves they came from: a union
// tags each item with its container and a join nests each side under its
// alias, and both are undone here, where they were done. A pass-through
// page belongs to its one leaf as it is. An item that does not fit the
// merge's shape is dropped.
func (p Plan) LeafItems(items []json.RawMessage) [][]json.RawMessage {
	byLeaf := make([][]json.RawMessage, len(p.Leaves))
	switch p.Merge {
	case UnionAll:
		p.untag(items, byLeaf)
	case HashJoin:
		p.uncombine(items, byLeaf)
	default:
		if len(byLeaf) > 0 {
			byLeaf[0] = items
		}
	}
	return byLeaf
}

func (p Plan) untag(items []json.RawMessage, byLeaf [][]json.RawMessage) {
	for _, item := range items {
		for i, leaf := range p.Leaves {
			if untagged, ok := untagRaw(item, leaf.Label()); ok {
				byLeaf[i] = append(byLeaf[i], untagged)
				break
			}
		}
	}
}

// untagRaw takes the container field tagRaw put at the head of an item.
func untagRaw(item json.RawMessage, label string) (json.RawMessage, bool) {
	field, err := containerField(label)
	if err != nil {
		return nil, false
	}
	rest, ok := bytes.CutPrefix(item, field)
	if !ok {
		return nil, false
	}
	if len(rest) > 0 && rest[0] == ',' {
		return append([]byte{'{'}, rest[1:]...), true
	}
	return json.RawMessage("{}"), true
}

func (p Plan) uncombine(items []json.RawMessage, byLeaf [][]json.RawMessage) {
	for _, item := range items {
		var combined map[string]json.RawMessage
		if err := json.Unmarshal(item, &combined); err != nil {
			continue
		}
		for i, leaf := range p.Leaves {
			if part, ok := combined[leaf.Alias]; ok {
				byLeaf[i] = append(byLeaf[i], part)
			}
		}
	}
}
