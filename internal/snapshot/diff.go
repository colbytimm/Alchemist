package snapshot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// ChangeKind is what happened to an item between two snapshots. Unchanged
// is a new version of the same content, a replace that wrote the document
// back as it was: a diff never lists it.
type ChangeKind int

const (
	Unchanged ChangeKind = iota
	Added
	Removed
	Modified
)

func (k ChangeKind) String() string {
	switch k {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Modified:
		return "modified"
	}
	return "unchanged"
}

func kindOf(c Change) ChangeKind {
	switch {
	case c.Before == nil:
		return Added
	case c.After == nil:
		return Removed
	case c.Before.Hash != c.After.Hash:
		return Modified
	}
	return Unchanged
}

type ItemChange struct {
	Kind     ChangeKind
	Key      Key
	Identity Identity
	Before   *Entry
	After    *Entry
}

type Diff struct {
	From       Record
	To         Record
	Items      []ItemChange
	Definition DefinitionDiff
}

func (d Diff) Count(kind ChangeKind) int {
	count := 0
	for _, item := range d.Items {
		if item.Kind == kind {
			count++
		}
	}
	return count
}

// Diff compares the snapshots from and to, by id, reading only the change
// sets between them: a neighbor's diff is one file, and no diff reads the
// full manifest. from may be the newer of the two.
func (s *Store) Diff(from, to string) (Diff, error) {
	fromIndex, err := s.indexOf(from)
	if err != nil {
		return Diff{}, err
	}
	toIndex, err := s.indexOf(to)
	if err != nil {
		return Diff{}, err
	}
	changes, err := s.changesBetween(min(fromIndex, toIndex), max(fromIndex, toIndex))
	if err != nil {
		return Diff{}, err
	}
	if fromIndex > toIndex {
		changes = changes.Invert()
	}
	d := Diff{From: s.records[fromIndex], To: s.records[toIndex]}
	if d.Items, err = itemChanges(changes); err != nil {
		return Diff{}, err
	}
	d.Definition, err = s.definitionDiff(d.From, d.To)
	return d, err
}

// changesBetween composes the change sets that lead from the snapshot at
// index from to the one at index to.
func (s *Store) changesBetween(from, to int) (ChangeSet, error) {
	var composed ChangeSet
	for i := from + 1; i <= to; i++ {
		c, err := s.changeSet(s.records[i])
		if err != nil {
			return nil, err
		}
		composed = Compose(composed, c)
	}
	return composed, nil
}

func itemChanges(changes ChangeSet) ([]ItemChange, error) {
	var items []ItemChange
	for _, change := range changes {
		kind := kindOf(change)
		if kind == Unchanged {
			continue
		}
		identity, err := change.Key.Identity()
		if err != nil {
			return nil, err
		}
		items = append(items, ItemChange{Kind: kind, Key: change.Key, Identity: identity, Before: change.Before, After: change.After})
	}
	return items, nil
}

// MovedIDs lists the ids that were removed under one partition key and
// added under another: in Cosmos that is two items, and the diff says so.
func (d Diff) MovedIDs() []string {
	removed := map[string]bool{}
	for _, item := range d.Items {
		if item.Kind == Removed {
			removed[item.Identity.ID] = true
		}
	}
	var moved []string
	for _, item := range d.Items {
		if item.Kind == Added && removed[item.Identity.ID] {
			moved = append(moved, item.Identity.ID)
		}
	}
	slices.Sort(moved)
	return slices.Compact(moved)
}

// ItemBodies reads the canonical bodies either side of an item change; the
// side that does not exist is nil.
func (s *Store) ItemBodies(change ItemChange) (before, after []byte, err error) {
	if change.Before != nil {
		if before, err = s.Body(change.Before.Hash); err != nil {
			return nil, nil, err
		}
	}
	if change.After != nil {
		if after, err = s.Body(change.After.Hash); err != nil {
			return nil, nil, err
		}
	}
	return before, after, nil
}

// DefinitionState is whether two snapshots' definitions could be compared.
type DefinitionState int

const (
	DefinitionCompared DefinitionState = iota
	// DefinitionNotCaptured is a snapshot taken by a connection that could
	// not read its container's definition.
	DefinitionNotCaptured
	// DefinitionIncomparable is two definitions from different backends.
	DefinitionIncomparable
)

// DefinitionDiff lists each setting that changed as "setting: what
// happened", with the backend's own paths shown verbatim.
type DefinitionDiff struct {
	State   DefinitionState
	Changes []string
}

func (s *Store) definitionDiff(from, to Record) (DefinitionDiff, error) {
	if from.Definition == "" || to.Definition == "" {
		return DefinitionDiff{State: DefinitionNotCaptured}, nil
	}
	if from.Definition == to.Definition {
		return DefinitionDiff{}, nil
	}
	before, err := s.definition(from.Definition)
	if err != nil {
		return DefinitionDiff{}, err
	}
	after, err := s.definition(to.Definition)
	if err != nil {
		return DefinitionDiff{}, err
	}
	return compareDefinitions(before, after)
}

func (s *Store) definition(hash string) (definitionDocument, error) {
	h, err := pack.ParseHash(hash)
	if err != nil {
		return definitionDocument{}, err
	}
	body, err := s.Body(h)
	if err != nil {
		return definitionDocument{}, err
	}
	var document definitionDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return definitionDocument{}, fmt.Errorf("snapshot: definition %s: %w: %w", hash, ErrCorrupt, err)
	}
	return document, nil
}

func compareDefinitions(before, after definitionDocument) (DefinitionDiff, error) {
	if before.Backend != after.Backend {
		return DefinitionDiff{State: DefinitionIncomparable}, nil
	}
	var d DefinitionDiff
	if !slices.Equal(before.PartitionKeys, after.PartitionKeys) {
		d.Changes = append(d.Changes, fmt.Sprintf("partition key: %s → %s",
			strings.Join(before.PartitionKeys, ","), strings.Join(after.PartitionKeys, ",")))
	}
	if throughputText(before.Throughput) != throughputText(after.Throughput) {
		d.Changes = append(d.Changes, fmt.Sprintf("throughput: %s → %s", throughputText(before.Throughput), throughputText(after.Throughput)))
	}
	policies, err := Structural(orEmpty(before.Policies), orEmpty(after.Policies))
	if err != nil {
		return DefinitionDiff{}, err
	}
	for _, change := range policies {
		d.Changes = append(d.Changes, change.Path+": "+change.Summary())
	}
	return d, nil
}

func orEmpty(document json.RawMessage) json.RawMessage {
	if len(document) == 0 {
		return json.RawMessage(`{}`)
	}
	return document
}

func throughputText(t *throughputRecord) string {
	switch {
	case t == nil:
		return "not read"
	case t.RUs > 0:
		return fmt.Sprintf("%d RU/s %s", t.RUs, t.Mode)
	}
	return t.Mode
}

// Summary describes a definition as a line of the list shows it.
func (d DefinitionDiff) Summary() string {
	switch d.State {
	case DefinitionNotCaptured:
		return "definition not captured"
	case DefinitionIncomparable:
		return "definitions from different backends"
	}
	switch len(d.Changes) {
	case 0:
		return "definition unchanged"
	case 1:
		return "definition: 1 setting"
	}
	return fmt.Sprintf("definition: %d settings", len(d.Changes))
}

// DefinitionChanged reports whether record's definition differs from its
// parent's, by hash: an inserted item can never trip it.
func (s *Store) DefinitionChanged(record Record) bool {
	i, err := s.indexOf(record.ID)
	if err != nil || i == 0 {
		return false
	}
	return record.Definition != s.records[i-1].Definition
}
