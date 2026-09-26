package mutate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

type patchEntry struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

// operation is the write sent for t: a patch of every SET and of each
// UNSET the item had a path for, on condition that the item still matches
// the statement's WHERE, still has those paths, and still has an object or
// an array where each nested SET goes. An item changed since it was
// selected so that it no longer matches is left alone by the service, not
// written.
func operation(m query.Mutation, t Target) adapter.Operation {
	entries := make([]patchEntry, 0, m.Operations())
	var guards []string
	for _, a := range m.Assignments {
		entries = append(entries, patchEntry{Op: "set", Path: a.Path.Pointer(), Value: a.Value})
		if guard, ok := parentGuard(a.Path); ok {
			guards = append(guards, guard)
		}
	}
	for i, path := range m.Removals {
		if t.lacks(i) {
			continue
		}
		entries = append(entries, patchEntry{Op: "remove", Path: path.Pointer()})
		if ref, ok := path.Dotted(); ok {
			guards = append(guards, "IS_DEFINED("+ref+")")
		}
	}
	body, _ := json.Marshal(entries) // strings and raw JSON a parser produced always marshal
	return adapter.Operation{Kind: adapter.OperationPatch, ID: t.ID, Body: body, Condition: condition(m, guards)}
}

// condition is the patch condition: the statement's WHERE, then each
// guard. A WHERE of true is left out when there are guards: the vNext
// emulator refuses a bare true beside an AND.
func condition(m query.Mutation, guards []string) string {
	head := "FROM " + m.Alias + " WHERE "
	if m.EveryItem && len(guards) > 0 {
		return head + strings.Join(guards, " AND ")
	}
	return head + "(" + m.Where + ")" + strings.Join(append([]string{""}, guards...), " AND ")
}

// Confirmation is what the review asks to have typed before the job may
// start: the container's name, and for a statement over every item, the
// number of items too.
func Confirmation(m query.Mutation, count int) string {
	container := m.Target[len(m.Target)-1]
	if m.EveryItem {
		return fmt.Sprintf("%s %d", container, count)
	}
	return container
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

// parentGuard is the check a patch's condition makes of where a nested SET
// goes: an object for a named field, an array for an index. A parent that
// cannot be written as a.b.c has none, since the vNext emulator refuses an
// index or a bracketed name in a patch condition; the selection's own
// check covers it.
func parentGuard(path query.FieldPath) (string, bool) {
	parent, ok := path.Parent()
	if !ok {
		return "", false
	}
	ref, ok := parent.Dotted()
	if !ok {
		return "", false
	}
	if path.Steps[len(path.Steps)-1].IsIndex {
		return "IS_ARRAY(" + ref + ")", true
	}
	return "IS_OBJECT(" + ref + ")", true
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
// array's end the service appends, condition or not, and a second run
// appends again.
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
