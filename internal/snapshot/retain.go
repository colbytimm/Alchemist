package snapshot

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// Delete removes the snapshot called id, then collects garbage. Every
// other snapshot's contents stay as they were: the oldest takes its child's
// change set with it, a middle one's is composed into its child's, and the
// head's is undone from the manifest to make its parent the head.
func (s *Store) Delete(id string, now time.Time) error {
	return s.maintain(now, func() error { return s.remove(id) })
}

// Policy is what a prune keeps: the newest KeepLast snapshots, and the
// newest of each of the last KeepDaily UTC days. The union is kept, and
// the head always is.
type Policy struct {
	KeepLast  int
	KeepDaily int
}

// Pruned lists what Prune would delete, oldest first.
func (s *Store) Pruned(policy Policy, now time.Time) []Record {
	keep := map[string]bool{}
	for i := max(len(s.records)-max(policy.KeepLast, 1), 0); i < len(s.records); i++ {
		keep[s.records[i].ID] = true
	}
	today := now.UTC().Truncate(24 * time.Hour)
	oldestDay := today.AddDate(0, 0, 1-policy.KeepDaily)
	newestOfDay := map[time.Time]string{}
	for _, record := range s.records {
		day := record.Time().UTC().Truncate(24 * time.Hour)
		if policy.KeepDaily > 0 && !day.Before(oldestDay) {
			newestOfDay[day] = record.ID
		}
	}
	for _, id := range newestOfDay {
		keep[id] = true
	}
	var pruned []Record
	for _, record := range s.records {
		if !keep[record.ID] {
			pruned = append(pruned, record)
		}
	}
	return pruned
}

// Prune deletes what policy does not keep, then collects garbage once.
func (s *Store) Prune(policy Policy, now time.Time) ([]Record, error) {
	var pruned []Record
	err := s.maintain(now, func() error {
		pruned = s.Pruned(policy, now)
		for _, record := range pruned {
			if err := s.remove(record.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return pruned, err
}

// maintain runs change under the lock, on the records as they are on disk,
// and collects garbage after it: under the lock, no capture can reuse a
// body that is about to go.
func (s *Store) maintain(now time.Time, change func() error) error {
	dir := s.loc.Dir()
	lock, err := takeLock(dir, now)
	if err != nil {
		return err
	}
	if s.records, err = readRecords(dir); err == nil {
		if err = change(); err == nil {
			err = s.collect(now)
		}
	}
	return errors.Join(err, s.Close(), lock.release())
}

func (s *Store) remove(id string) error {
	i, err := s.indexOf(id)
	if err != nil {
		return err
	}
	switch {
	case len(s.records) == 1:
		err = s.removeOnly()
	case i == len(s.records)-1:
		err = s.removeHead()
	case i == 0:
		err = s.removeOldest()
	default:
		err = s.removeMiddle(i)
	}
	if err != nil {
		return err
	}
	s.records = slices.Delete(slices.Clone(s.records), i, i+1)
	return nil
}

func (s *Store) removeOnly() error {
	dir, record := s.loc.Dir(), s.records[0]
	if err := pack.RemoveDurably(recordPath(dir, record.ID)); err != nil {
		return err
	}
	return removeAll([]string{manifestPath(dir, record.ID)})
}

// removeHead writes the parent's manifest before the head's record goes,
// which is the commit: until then the extra manifest is debris Open removes.
func (s *Store) removeHead() error {
	dir := s.loc.Dir()
	head, parent := s.records[len(s.records)-1], s.records[len(s.records)-2]
	m, err := s.headManifest()
	if err != nil {
		return err
	}
	changes, err := s.changeSet(head)
	if err != nil {
		return err
	}
	m.Undo(changes)
	if err := writeManifest(manifestPath(dir, parent.ID), m); err != nil {
		return err
	}
	if err := pack.RemoveDurably(recordPath(dir, head.ID)); err != nil {
		return err
	}
	return removeAll([]string{manifestPath(dir, head.ID), filepath.Join(dir, changesDir, changesName(parent.ID, head.ID))})
}

// removeOldest detaches the oldest by making its child the first snapshot,
// which is the commit; a record left detached is debris Open removes.
func (s *Store) removeOldest() error {
	dir := s.loc.Dir()
	oldest, child := s.records[0], s.records[1]
	child.Parent = ""
	child.Added, child.Removed, child.Modified = child.Items, 0, 0
	if err := writeRecord(dir, child); err != nil {
		return err
	}
	s.records[1] = child
	return removeAll([]string{recordPath(dir, oldest.ID), filepath.Join(dir, changesDir, changesName(oldest.ID, child.ID))})
}

// removeMiddle composes the snapshot's change set into its child's, under
// a name nothing reads yet, then points the child at the new parent, which
// is the commit.
func (s *Store) removeMiddle(i int) error {
	dir := s.loc.Dir()
	parent, record, child := s.records[i-1], s.records[i], s.records[i+1]
	first, err := s.changeSet(record)
	if err != nil {
		return err
	}
	second, err := s.changeSet(child)
	if err != nil {
		return err
	}
	composed := Compose(first, second)
	if err := writeChanges(filepath.Join(dir, changesDir, changesName(parent.ID, child.ID)), composed); err != nil {
		return err
	}
	child.Parent = parent.ID
	child.Added, child.Removed, child.Modified = tally(composed)
	if err := writeRecord(dir, child); err != nil {
		return err
	}
	s.records[i+1] = child
	return removeAll([]string{
		recordPath(dir, record.ID),
		filepath.Join(dir, changesDir, changesName(parent.ID, record.ID)),
		filepath.Join(dir, changesDir, changesName(record.ID, child.ID)),
	})
}

func tally(changes ChangeSet) (added, removed, modified int64) {
	for _, change := range changes {
		switch kindOf(change) {
		case Added:
			added++
		case Removed:
			removed++
		case Modified:
			modified++
		}
	}
	return added, removed, modified
}

// collect marks every body a surviving snapshot can reach, the head's
// manifest, every change set's before and after, every definition, and has
// the packs reclaim the rest. The records are synced first: a record a
// power loss brought back would name bodies already reclaimed.
func (s *Store) collect(now time.Time) error {
	if err := pack.SyncDir(filepath.Join(s.loc.Dir(), recordsDir)); err != nil {
		return err
	}
	live, err := s.reachable()
	if err != nil {
		return err
	}
	_, err = pack.Collect(filepath.Join(s.loc.Dir(), packsDir), newID(now)+"-gc", live)
	return err
}

func (s *Store) reachable() (map[pack.Hash]bool, error) {
	m, err := s.headManifest()
	if err != nil {
		return nil, err
	}
	live := make(map[pack.Hash]bool, len(m))
	for _, entry := range m {
		live[entry.Hash] = true
	}
	for _, record := range s.records {
		if err := s.markRecord(record, live); err != nil {
			return nil, err
		}
	}
	return live, nil
}

func (s *Store) markRecord(record Record, live map[pack.Hash]bool) error {
	if record.Definition != "" {
		h, err := pack.ParseHash(record.Definition)
		if err != nil {
			return fmt.Errorf("snapshot: %s: record %s: %w", s.loc, record.ID, err)
		}
		live[h] = true
	}
	if record.Parent == "" {
		return nil
	}
	changes, err := s.changeSet(record)
	if err != nil {
		return err
	}
	for _, change := range changes {
		for _, side := range []*Entry{change.Before, change.After} {
			if side != nil {
				live[side.Hash] = true
			}
		}
	}
	return nil
}
