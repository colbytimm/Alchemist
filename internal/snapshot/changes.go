package snapshot

import (
	"cmp"
	"fmt"
	"slices"
)

// Change is one key whose entry differs between two snapshots: nil Before
// is an item that did not exist, nil After one that no longer does. A
// change whose hashes agree is a new version of the same content, kept so
// that a change set turns one manifest into the other exactly.
type Change struct {
	Key    Key
	Before *Entry
	After  *Entry
}

// ChangeSet is every change from one snapshot to another, sorted by key.
type ChangeSet []Change

const (
	hasBefore byte = 1 << iota
	hasAfter
)

// Changes is what turns parent into child.
func Changes(parent, child Manifest) ChangeSet {
	var set ChangeSet
	for k, before := range parent {
		after, ok := child[k]
		switch {
		case !ok:
			set = append(set, Change{Key: k, Before: &before})
		case after != before:
			set = append(set, Change{Key: k, Before: &before, After: &after})
		}
	}
	for k, after := range child {
		if _, ok := parent[k]; !ok {
			set = append(set, Change{Key: k, After: &after})
		}
	}
	return set.sorted()
}

func (c ChangeSet) sorted() ChangeSet {
	slices.SortFunc(c, func(a, b Change) int { return cmp.Compare(a.Key, b.Key) })
	return c
}

// Apply moves m forward through c, in place.
func (m Manifest) Apply(c ChangeSet) {
	for _, change := range c {
		if change.After == nil {
			delete(m, change.Key)
			continue
		}
		m[change.Key] = *change.After
	}
}

// Undo moves m back through c, in place.
func (m Manifest) Undo(c ChangeSet) {
	for _, change := range c {
		if change.Before == nil {
			delete(m, change.Key)
			continue
		}
		m[change.Key] = *change.Before
	}
}

// Compose is first then second as one change set: per key, the first
// Before and the last After, dropping a key that ends where it began.
func Compose(first, second ChangeSet) ChangeSet {
	byKey := make(map[Key]Change, len(first)+len(second))
	for _, change := range first {
		byKey[change.Key] = change
	}
	for _, change := range second {
		if earlier, ok := byKey[change.Key]; ok {
			change.Before = earlier.Before
		}
		byKey[change.Key] = change
	}
	var composed ChangeSet
	for _, change := range byKey {
		if !sameEntry(change.Before, change.After) {
			composed = append(composed, change)
		}
	}
	return composed.sorted()
}

func (c ChangeSet) Invert() ChangeSet {
	inverted := make(ChangeSet, len(c))
	for i, change := range c {
		inverted[i] = Change{Key: change.Key, Before: change.After, After: change.Before}
	}
	return inverted
}

func sameEntry(a, b *Entry) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func writeChanges(path string, c ChangeSet) error {
	return writeStream(path, changesKind, len(c), func(emit func([]byte) error) error {
		for _, change := range c {
			if err := emit(encodeChange(change)); err != nil {
				return err
			}
		}
		return nil
	})
}

func encodeChange(change Change) []byte {
	encoded := appendKey(nil, change.Key)
	var flags byte
	if change.Before != nil {
		flags |= hasBefore
	}
	if change.After != nil {
		flags |= hasAfter
	}
	encoded = append(encoded, flags)
	if change.Before != nil {
		encoded = appendEntry(encoded, *change.Before)
	}
	if change.After != nil {
		encoded = appendEntry(encoded, *change.After)
	}
	return encoded
}

func readChanges(path string) (ChangeSet, error) {
	var c ChangeSet
	err := readStream(path, changesKind, func(r *streamReader) error {
		change, err := decodeChange(r)
		if err != nil {
			return err
		}
		if len(c) > 0 && c[len(c)-1].Key >= change.Key {
			return fmt.Errorf("change set out of order: %w", ErrCorrupt)
		}
		c = append(c, change)
		return nil
	})
	return c, err
}

func decodeChange(r *streamReader) (Change, error) {
	k, err := r.key()
	if err != nil {
		return Change{}, err
	}
	flags, err := r.ReadByte()
	if err != nil || flags == 0 || flags&^(hasBefore|hasAfter) != 0 {
		return Change{}, fmt.Errorf("change flags unreadable: %w", ErrCorrupt)
	}
	change := Change{Key: k}
	if flags&hasBefore != 0 {
		before, err := r.entry()
		if err != nil {
			return Change{}, err
		}
		change.Before = &before
	}
	if flags&hasAfter != 0 {
		after, err := r.entry()
		if err != nil {
			return Change{}, err
		}
		change.After = &after
	}
	return change, nil
}
