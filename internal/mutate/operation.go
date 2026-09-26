package mutate

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

type patchEntry struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

// operation is the write sent for t: a delete on the version the selection
// read, or a conditional patch.
func operation(m query.Mutation, t Target) adapter.Operation {
	if m.Kind == query.MutationDelete {
		return adapter.Operation{Kind: adapter.OperationDelete, ID: t.ID, IfMatch: t.Version}
	}
	return patch(m, t)
}

// patch is a patch of every SET and of each UNSET the item had a path for,
// on condition that the item still matches the statement's WHERE, still
// has those paths, and still has where each nested SET goes. An item
// changed since it was selected so that it no longer matches is left alone
// by the service, not written. The guards are IS_DEFINED alone: the vNext
// emulator refuses IS_OBJECT, IS_ARRAY and ARRAY_LENGTH in a patch
// condition.
func patch(m query.Mutation, t Target) adapter.Operation {
	entries := make([]patchEntry, 0, m.Operations())
	condition := fmt.Sprintf("FROM %s WHERE (%s)", m.Alias, m.Where)
	for _, a := range m.Assignments {
		entries = append(entries, patchEntry{Op: "set", Path: a.Path.Pointer(), Value: a.Value})
		if guard, ok := conditionGuard(a.Path); ok {
			condition += " AND IS_DEFINED(" + guard.String() + ")"
		}
	}
	for i, path := range m.Removals {
		if t.lacks(i) {
			continue
		}
		entries = append(entries, patchEntry{Op: "remove", Path: path.Pointer()})
		condition += " AND IS_DEFINED(" + path.String() + ")"
	}
	body, _ := json.Marshal(entries) // strings and raw JSON a parser produced always marshal
	return adapter.Operation{Kind: adapter.OperationPatch, ID: t.ID, Body: body, Condition: condition}
}

// Confirmation is what the review asks to have typed before the job may
// start: the container's name, and the number of items too for a delete or
// for a statement over every item.
func Confirmation(m query.Mutation, count int) string {
	container := m.Target[len(m.Target)-1]
	if m.EveryItem || m.Kind == query.MutationDelete {
		return fmt.Sprintf("%s %d", container, count)
	}
	return container
}

// PlanningCharge is the request units a review plans on for one write of a
// small item by a statement of kind.
func PlanningCharge(kind query.MutationKind) int {
	if kind == query.MutationDelete {
		return PlanningChargePerDelete
	}
	return PlanningChargePerPatch
}

// Changes lists what the statement does to every item it writes, in the
// order it does it: "set /status "archived"", "remove /tmp".
func Changes(m query.Mutation) []string {
	changes := make([]string, 0, m.Operations())
	for _, a := range m.Assignments {
		changes = append(changes, "set "+a.Path.Pointer()+" "+string(a.Value))
	}
	for _, path := range m.Removals {
		changes = append(changes, "remove "+path.Pointer())
	}
	return changes
}

// nestedSet reports a SET below the top level, whose parent a patch needs
// on the item: set creates only the last step of its path.
func nestedSet(m query.Mutation) bool {
	for _, a := range m.Assignments {
		if _, ok := a.Path.Parent(); ok {
			return true
		}
	}
	return false
}

// conditionGuard is the parent of path a patch's condition re-asserts. A
// parent reached through an array index has none: the service refuses an
// index in a patch condition, so only the selection's check covers it.
func conditionGuard(path query.FieldPath) (query.FieldPath, bool) {
	parent, ok := path.Parent()
	if !ok || slices.ContainsFunc(parent.Steps, func(step query.PathStep) bool { return step.IsIndex }) {
		return query.FieldPath{}, false
	}
	return parent, true
}

// placesEverySet reports whether item has somewhere to put every SET of m.
func placesEverySet(item json.RawMessage, m query.Mutation) bool {
	for _, a := range m.Assignments {
		if !placeable(item, a.Path) {
			return false
		}
	}
	return true
}

// placeable reports whether a set of path would succeed on item, and
// succeed the same however often it runs: its parent is an object for a
// named field, or an array holding the element an index replaces. Past an
// array's end the service appends, a second run appending again, and with
// a condition it refuses the patch.
func placeable(item json.RawMessage, path query.FieldPath) bool {
	parent, ok := path.Parent()
	if !ok {
		return true
	}
	value, found := valueAt(item, parent.Steps)
	if !found {
		return false
	}
	if last := path.Steps[len(path.Steps)-1]; last.IsIndex {
		var array []json.RawMessage
		return json.Unmarshal(value, &array) == nil && last.Index < len(array)
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

// valueAt is the value at steps under item, and false where the item has
// nothing there.
func valueAt(item json.RawMessage, steps []query.PathStep) (json.RawMessage, bool) {
	value := item
	for _, step := range steps {
		if step.IsIndex {
			var array []json.RawMessage
			if json.Unmarshal(value, &array) != nil || step.Index >= len(array) {
				return nil, false
			}
			value = array[step.Index]
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil {
			return nil, false
		}
		next, ok := object[step.Name]
		if !ok {
			return nil, false
		}
		value = next
	}
	return value, true
}

// formatCount writes n with thousands separators: 10,000.
func formatCount(n int) string {
	digits := fmt.Sprint(n)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}
