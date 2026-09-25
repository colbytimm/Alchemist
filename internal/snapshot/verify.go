package snapshot

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

type VerifyOptions struct {
	// Deep decompresses every block and rehashes every body.
	Deep bool
	// RebuildIndex rewrites every pack's index from the pack first.
	RebuildIndex bool
}

type VerifyReport struct {
	Snapshots int
	Packs     int
	Bodies    int
}

// Verify checks the store under its lock: every file's first line and
// version, every CRC and trailer, that every change set leads from its
// parent to its snapshot, and that every body a snapshot names is in a
// pack. Every problem found is in the error, each wrapping ErrCorrupt or
// ErrUnknownFormat.
func (s *Store) Verify(options VerifyOptions, now time.Time) (VerifyReport, error) {
	dir := s.loc.Dir()
	if err := checkFormat(dir); err != nil {
		return VerifyReport{}, err
	}
	lock, err := takeLock(dir, now)
	if err != nil {
		return VerifyReport{}, err
	}
	report, err := s.verify(options)
	return report, errors.Join(err, s.Close(), lock.release())
}

func (s *Store) verify(options VerifyOptions) (VerifyReport, error) {
	dir := s.loc.Dir()
	records, err := readRecords(dir)
	if err != nil {
		return VerifyReport{}, err
	}
	s.records = records
	report := VerifyReport{Snapshots: len(records)}
	var problems []error
	if _, whole := chainFromHead(records); !whole || !linked(records) {
		problems = append(problems, fmt.Errorf("snapshot: %s: the records do not form one chain from the newest: %w", s.loc, ErrCorrupt))
	}
	live, err := s.walkChain()
	if err != nil {
		problems = append(problems, err)
	}
	packs, err := s.checkPacks(options)
	report.Packs = len(packs)
	if err != nil {
		problems = append(problems, err)
	}
	report.Bodies = len(live)
	problems = append(problems, s.checkReachable(live)...)
	return report, errors.Join(problems...)
}

// linked reports whether each record's parent is the one before it.
func linked(records []Record) bool {
	for i, record := range records {
		if i == 0 && record.Parent != "" || i > 0 && record.Parent != records[i-1].ID {
			return false
		}
	}
	return true
}

// walkChain undoes change sets from the head's manifest back to the oldest
// snapshot, checking at each step that the change set's After is what the
// manifest holds and that the manifest holds as many items as the record
// says. It returns every body a snapshot names.
func (s *Store) walkChain() (map[pack.Hash]bool, error) {
	m, err := s.headManifest()
	if err != nil {
		return nil, err
	}
	live := map[pack.Hash]bool{}
	for _, entry := range m {
		live[entry.Hash] = true
	}
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		if err := s.markRecord(record, live); err != nil {
			return live, err
		}
		if int64(len(m)) != record.Items {
			return live, fmt.Errorf("snapshot: %s: snapshot %s should hold %d items and holds %d: %w", s.loc, record.ID, record.Items, len(m), ErrCorrupt)
		}
		if record.Parent == "" {
			continue
		}
		changes, err := s.changeSet(record)
		if err != nil {
			return live, err
		}
		if err := leadsTo(changes, m); err != nil {
			return live, fmt.Errorf("snapshot: %s: change set %s: %w", s.loc, changesName(record.Parent, record.ID), err)
		}
		m.Undo(changes)
	}
	return live, nil
}

func leadsTo(changes ChangeSet, m Manifest) error {
	for _, change := range changes {
		held, ok := m[change.Key]
		if change.After == nil && ok || change.After != nil && (!ok || held != *change.After) {
			return fmt.Errorf("does not lead to the snapshot after it: %w", ErrCorrupt)
		}
	}
	return nil
}

func (s *Store) checkPacks(options VerifyOptions) ([]string, error) {
	dir := filepath.Join(s.loc.Dir(), packsDir)
	names, err := pack.Names(dir)
	if err != nil {
		return nil, err
	}
	depth := pack.Shallow
	if options.Deep {
		depth = pack.Deep
	}
	var problems []error
	for _, name := range names {
		if options.RebuildIndex {
			if err := pack.RebuildIndex(dir, name); err != nil {
				problems = append(problems, err)
				continue
			}
		}
		if err := pack.Check(dir, name, depth); err != nil {
			problems = append(problems, err)
		}
	}
	return names, errors.Join(problems...)
}

func (s *Store) checkReachable(live map[pack.Hash]bool) []error {
	set, err := pack.OpenSet(filepath.Join(s.loc.Dir(), packsDir))
	if err != nil {
		return []error{err}
	}
	defer func() { _ = set.Close() }() // read-only: a close failure loses nothing
	var problems []error
	for h := range live {
		if !set.Has(h) {
			problems = append(problems, fmt.Errorf("snapshot: %s: body %s is in no pack: %w", s.loc, h, ErrCorrupt))
		}
	}
	return problems
}
