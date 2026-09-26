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
// the statement's WHERE and still has those paths. An item changed since
// it was selected so that it no longer matches is left alone by the
// service, not written.
func operation(m query.Mutation, t Target) adapter.Operation {
	entries := make([]patchEntry, 0, m.Operations())
	for _, a := range m.Assignments {
		entries = append(entries, patchEntry{Op: "set", Path: a.Path.Pointer(), Value: a.Value})
	}
	condition := fmt.Sprintf("FROM %s WHERE (%s)", m.Alias, m.Where)
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
