package mutate

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/colbytimm/alchemist/internal/canonical"
	"github.com/colbytimm/alchemist/internal/query"
)

type ChangeKind int

const (
	Changed ChangeKind = iota + 1
	Added
	Removed
	Unchanged // a SET to the value the field already has
)

// FieldChange is what the statement would do to one path of one item.
type FieldChange struct {
	Kind   ChangeKind
	Path   string // a JSON Pointer
	Before json.RawMessage
	After  json.RawMessage
}

var errNotAnItem = errors.New("mutate: preview: the item is not a JSON object")

// Preview is what m would do to item, worked out locally: one change per
// path the patch would touch. An UNSET of a path the item lacks touches
// nothing, and is left out.
func Preview(item json.RawMessage, m query.Mutation) ([]FieldChange, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(item, &object) != nil {
		return nil, errNotAnItem
	}
	changes := make([]FieldChange, 0, m.Operations())
	for _, a := range m.Assignments {
		change := FieldChange{Kind: Added, Path: a.Path.Pointer(), After: a.Value}
		if before, ok := valueAt(item, a.Path.Steps); ok {
			change.Before, change.Kind = before, Changed
			if sameJSON(before, a.Value) {
				change.Kind = Unchanged
			}
		}
		changes = append(changes, change)
	}
	for _, path := range m.Removals {
		if before, ok := valueAt(item, path.Steps); ok {
			changes = append(changes, FieldChange{Kind: Removed, Path: path.Pointer(), Before: before})
		}
	}
	return changes, nil
}

func sameJSON(a, b json.RawMessage) bool {
	ca, errA := canonical.Marshal(a)
	cb, errB := canonical.Marshal(b)
	if errA != nil || errB != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca, cb)
}
