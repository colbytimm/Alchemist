package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// The names a snapshot argument may use besides an id or its prefix.
const (
	RefLatest   = "latest"
	RefPrevious = "previous"
)

// Store is one container's snapshots. Opening it reads the records; packs
// are opened on the first body read. It is not safe for concurrent use.
type Store struct {
	loc     Location
	records []Record
	bodies  *pack.Set
}

// Open reads the store at loc. A store that was never written holds no
// snapshots. When no capture or prune holds the lock, what an interrupted
// one left behind is removed first; no state ever needs repair by hand.
func Open(loc Location) (*Store, error) {
	dir := loc.Dir()
	found, err := exists(dir)
	if err != nil || !found {
		return &Store{loc: loc}, err
	}
	if err := checkFormat(dir); err != nil {
		return nil, err
	}
	if err := tidy(dir); err != nil {
		return nil, err
	}
	records, err := readRecords(dir)
	if err != nil {
		return nil, err
	}
	return &Store{loc: loc, records: records}, nil
}

func checkFormat(dir string) error {
	file, err := os.Open(filepath.Join(dir, formatFile)) // #nosec G304 -- the store's own FORMAT file
	if err != nil {
		return fmt.Errorf("snapshot: %s: %w", dir, err)
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	if _, err := pack.ReadHeader(file, formatKind); err != nil {
		return fmt.Errorf("snapshot: %s: %w", dir, err)
	}
	return nil
}

// createStore lays out a new store's directories and describes it.
func createStore(loc Location) error {
	dir := loc.Dir()
	for _, sub := range []string{recordsDir, changesDir, manifestsDir, packsDir} {
		if err := ensureDir(filepath.Join(dir, sub)); err != nil {
			return err
		}
	}
	if found, err := exists(filepath.Join(dir, formatFile)); err != nil || found {
		return err
	}
	meta := storeMeta{Format: documentFormat, Account: loc.Account, Database: loc.Database, Container: loc.Container}
	if err := writeJSON(filepath.Join(dir, storeFile), meta); err != nil {
		return err
	}
	return pack.WriteFile(filepath.Join(dir, formatFile), pack.Header(formatKind))
}

func (s *Store) Location() Location { return s.loc }

// Snapshots lists the store's snapshots, oldest first.
func (s *Store) Snapshots() []Record { return slices.Clone(s.records) }

func (s *Store) head() (Record, bool) {
	if len(s.records) == 0 {
		return Record{}, false
	}
	return s.records[len(s.records)-1], true
}

// Resolve finds the snapshot ref names: an id, a unique prefix of one,
// RefLatest or RefPrevious.
func (s *Store) Resolve(ref string) (Record, error) {
	n := len(s.records)
	switch {
	case ref == RefLatest && n > 0:
		return s.records[n-1], nil
	case ref == RefPrevious && n > 1:
		return s.records[n-2], nil
	case ref == RefLatest || ref == RefPrevious || ref == "":
		return Record{}, fmt.Errorf("snapshot: %s has no %s snapshot: %w", s.loc, ref, ErrNoSnapshot)
	}
	var matches []Record
	for _, record := range s.records {
		if record.ID == ref {
			return record, nil
		}
		if strings.HasPrefix(record.ID, ref) {
			matches = append(matches, record)
		}
	}
	switch len(matches) {
	case 0:
		return Record{}, fmt.Errorf("snapshot: %s has no snapshot %q: %w", s.loc, ref, ErrNoSnapshot)
	case 1:
		return matches[0], nil
	}
	return Record{}, fmt.Errorf("snapshot: %q names %d snapshots of %s: %w", ref, len(matches), s.loc, ErrNoSnapshot)
}

func (s *Store) indexOf(id string) (int, error) {
	i := slices.IndexFunc(s.records, func(r Record) bool { return r.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("snapshot: %s has no snapshot %q: %w", s.loc, id, ErrNoSnapshot)
	}
	return i, nil
}

// Body is the canonical body stored under h.
func (s *Store) Body(h pack.Hash) ([]byte, error) {
	if s.bodies == nil {
		set, err := pack.OpenSet(filepath.Join(s.loc.Dir(), packsDir))
		if err != nil {
			return nil, err
		}
		s.bodies = set
	}
	body, err := s.bodies.Read(h)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %s: %w", s.loc, err)
	}
	return body, nil
}

func (s *Store) Close() error {
	if s.bodies == nil {
		return nil
	}
	err := s.bodies.Close()
	s.bodies = nil
	return err
}

// changeSet reads the change set that leads to record from its parent.
func (s *Store) changeSet(record Record) (ChangeSet, error) {
	return readChanges(filepath.Join(s.loc.Dir(), changesDir, changesName(record.Parent, record.ID)))
}

func (s *Store) headManifest() (Manifest, error) {
	head, ok := s.head()
	if !ok {
		return Manifest{}, nil
	}
	return readManifest(manifestPath(s.loc.Dir(), head.ID))
}

func manifestPath(dir, id string) string {
	return filepath.Join(dir, manifestsDir, id+manifestExt)
}

// Contents is what the snapshot called id held: the head's manifest, with
// change sets undone back to it.
func (s *Store) Contents(id string) (Manifest, error) {
	target, err := s.indexOf(id)
	if err != nil {
		return nil, err
	}
	m, err := s.headManifest()
	if err != nil {
		return nil, err
	}
	for i := len(s.records) - 1; i > target; i-- {
		c, err := s.changeSet(s.records[i])
		if err != nil {
			return nil, err
		}
		m.Undo(c)
	}
	return m, nil
}

// tidy removes what an interrupted capture, delete or prune left, unless
// one is running now: it owns its debris until it ends.
func tidy(dir string) error {
	lock, err := takeLock(dir, time.Now())
	if errors.Is(err, ErrLocked) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(removeDebris(dir), lock.release())
}

func removeDebris(dir string) error {
	if err := removeTemporary(dir); err != nil {
		return err
	}
	records, err := readRecords(dir)
	if err != nil {
		return err
	}
	records, err = removeDetached(dir, records)
	if err != nil {
		return err
	}
	if err := removeUnnamed(filepath.Join(dir, changesDir), changesExt, wantedChanges(records)); err != nil {
		return err
	}
	return removeStaleManifests(dir, records)
}

func removeTemporary(dir string) error {
	for _, sub := range []string{".", recordsDir, changesDir, manifestsDir, packsDir} {
		temps, err := filepath.Glob(filepath.Join(dir, sub, "*"+pack.TempExt))
		if err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		if err := removeAll(temps); err != nil {
			return err
		}
	}
	return nil
}

// removeDetached removes the records an interrupted delete had already
// cut out of the chain from the head. It removes nothing unless that chain
// is whole, back to a snapshot with no parent: a chain broken any other way
// is corruption for verify to report, not debris.
func removeDetached(dir string, records []Record) ([]Record, error) {
	chain, whole := chainFromHead(records)
	if !whole {
		return records, nil
	}
	var kept []Record
	for _, record := range records {
		if chain[record.ID] {
			kept = append(kept, record)
			continue
		}
		if err := removeAll([]string{recordPath(dir, record.ID)}); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

// chainFromHead is every record the newest one reaches through its
// parents, and whether that walk ends at a record with no parent.
func chainFromHead(records []Record) (map[string]bool, bool) {
	byID := map[string]Record{}
	for _, record := range records {
		byID[record.ID] = record
	}
	chain := map[string]bool{}
	if len(records) == 0 {
		return chain, true
	}
	for current, ok := records[len(records)-1], true; ok; current, ok = byID[current.Parent] {
		if chain[current.ID] {
			return chain, false
		}
		chain[current.ID] = true
		if current.Parent == "" {
			return chain, true
		}
	}
	return chain, false
}

func wantedChanges(records []Record) map[string]bool {
	wanted := map[string]bool{}
	for _, record := range records {
		if record.Parent != "" {
			wanted[changesName(record.Parent, record.ID)] = true
		}
	}
	return wanted
}

// removeStaleManifests keeps the head's manifest alone, once it is there:
// a capture that published leaves its parent's behind until it removes it.
func removeStaleManifests(dir string, records []Record) error {
	wanted := map[string]bool{}
	if len(records) > 0 {
		head := records[len(records)-1].ID + manifestExt
		found, err := exists(filepath.Join(dir, manifestsDir, head))
		if err != nil || !found {
			return err
		}
		wanted[head] = true
	}
	return removeUnnamed(filepath.Join(dir, manifestsDir), manifestExt, wanted)
}

func removeUnnamed(dir, ext string, wanted map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	var unwanted []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ext) && !wanted[entry.Name()] {
			unwanted = append(unwanted, filepath.Join(dir, entry.Name()))
		}
	}
	return removeAll(unwanted)
}

func removeAll(paths []string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("snapshot: %w", err)
		}
	}
	return nil
}
