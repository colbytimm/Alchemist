package query

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// LeafItems sorts a page's items back to the leaves they came from: a union
// tags each item with its container and a join nests each input under its
// alias, and both are undone here, where they were done. A pass-through
// page belongs to its one leaf as it is. An item that does not fit the
// plan's shape is dropped, and so is every item of a CTE, which is not an
// item of its container.
func (p Plan) LeafItems(items []json.RawMessage) [][]json.RawMessage {
	byLeaf := make([][]json.RawMessage, len(p.Leaves))
	switch root := p.Root.(type) {
	case *Scan:
		if root.Leaf >= 0 && root.Leaf < len(byLeaf) {
			byLeaf[root.Leaf] = items
		}
	case *Union:
		p.untag(root, items, byLeaf)
	case *Join:
		p.separate(root, items, byLeaf)
	}
	return byLeaf
}

// ObservedFields are the fields a page's items show of the containers they
// came from, by leaf. Only items as stored are fields of a container: a
// projection names what the query made of them. A join input is the
// exception, since a projected half still holds top-level fields of its
// container, apart from one the SELECT list renamed.
func (p Plan) ObservedFields(items []json.RawMessage) map[int][]adapter.Field {
	observed := map[int][]adapter.Field{}
	renamed := p.renamedFields()
	for leaf, leafItems := range p.LeafItems(items) {
		if len(leafItems) == 0 || !p.joined() && !p.Leaves[leaf].WholeItems() {
			continue
		}
		fields := adapter.FlattenFields(leafItems)
		observed[leaf] = slices.DeleteFunc(fields, func(f adapter.Field) bool { return slices.Contains(renamed[leaf], f.Path) })
	}
	return observed
}

func (p Plan) joined() bool {
	_, ok := p.Root.(*Join)
	return ok
}

// renamedFields are the names the SELECT list of a join gave to fields of
// each leaf it reads.
func (p Plan) renamedFields() map[int][]string {
	join, ok := p.Root.(*Join)
	if !ok {
		return nil
	}
	renamed := map[int][]string{}
	for _, column := range join.Columns {
		leaf, ok := p.containerLeaf(join, column.Side)
		if ok && column.As != "" {
			renamed[leaf] = append(renamed[leaf], column.As)
		}
	}
	return renamed
}

// containerLeaf is the leaf an input of join reads when that input is a
// container of this query, not a CTE.
func (p Plan) containerLeaf(join *Join, side int) (int, bool) {
	if side < 0 || side >= len(join.Inputs) {
		return 0, false
	}
	scan, ok := join.Inputs[side].Rows.(*Scan)
	if !ok || scan.Leaf < 0 || scan.Leaf >= len(p.Leaves) || p.Leaves[scan.Leaf].Name != "" {
		return 0, false
	}
	return scan.Leaf, true
}

func (p Plan) untag(union *Union, items []json.RawMessage, byLeaf [][]json.RawMessage) {
	for _, item := range items {
		for _, i := range union.Leaves {
			if i < 0 || i >= len(byLeaf) {
				continue
			}
			if untagged, ok := untagRaw(item, p.Leaves[i].Label()); ok {
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

// separate files each input's part of a joined item under its leaf; an
// input that is a CTE has none.
func (p Plan) separate(join *Join, items []json.RawMessage, byLeaf [][]json.RawMessage) {
	for _, item := range items {
		var combined map[string]json.RawMessage
		if err := json.Unmarshal(item, &combined); err != nil {
			continue
		}
		for side, input := range join.Inputs {
			leaf, isContainer := p.containerLeaf(join, side)
			part, ok := combined[input.Alias]
			if isContainer && ok {
				byLeaf[leaf] = append(byLeaf[leaf], part)
			}
		}
	}
}
