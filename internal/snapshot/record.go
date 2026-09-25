package snapshot

import (
	"encoding/json"
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

// idLayout names a snapshot for the moment its capture started, in UTC:
// sortable, and meaningful to ls.
const idLayout = "20060102T150405Z"

// Mode is how a capture read its container.
type Mode string

const (
	ModeFull        Mode = "full"
	ModeIncremental Mode = "incremental"
)

// Record is one snapshot as a list needs it. Its file is published by
// rename, which is what makes the snapshot exist.
type Record struct {
	Format int    `json:"format"`
	ID     string `json:"id"`
	// Parent is the snapshot this one's change set leads from; empty for
	// the oldest.
	Parent string `json:"parent,omitempty"`
	Group  string `json:"group,omitempty"`
	Note   string `json:"note,omitempty"`
	// Started and Finished bound the window the capture read in: a
	// snapshot of a live container is not a point in time.
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Mode     Mode      `json:"mode"`
	Items    int64     `json:"items"`
	Added    int64     `json:"added"`
	Removed  int64     `json:"removed"`
	Modified int64     `json:"modified"`
	// LogicalBytes is the canonical bodies' total, what an export weighs;
	// StoredBytes is what this capture added to the disk.
	LogicalBytes  int64   `json:"logicalBytes"`
	StoredBytes   int64   `json:"storedBytes"`
	ReadBytes     int64   `json:"readBytes"`
	RequestCharge float64 `json:"requestCharge"`
	// Definition is the hash of the definition blob; empty when the
	// connection could not read one.
	Definition string       `json:"definition,omitempty"`
	Size       *SizeReading `json:"size,omitempty"`
	// Resume is reserved for a change feed continuation.
	Resume string `json:"resume,omitempty"`
}

// SizeReading is what the backend said the container held when the
// capture began. It is a reading, never part of the definition's hash.
type SizeReading struct {
	Items int64 `json:"items"`
	Bytes int64 `json:"bytes"`
}

func (r Record) Window() time.Duration { return r.Finished.Sub(r.Started) }

func (r Record) Changes() int64 { return r.Added + r.Removed + r.Modified }

func (r Record) Time() time.Time {
	t, err := time.Parse(idLayout, r.ID)
	if err != nil {
		return r.Started
	}
	return t
}

func newID(started time.Time) string { return started.UTC().Format(idLayout) }

// nextID is the id for a capture starting at started: its time, or one
// second past the newest record when a clock that stepped back, or two
// captures in one second, would otherwise repeat or reorder an id.
func nextID(started time.Time, newest string) string {
	id := newID(started)
	if newest == "" || id > newest {
		return id
	}
	last, err := time.Parse(idLayout, newest)
	if err != nil {
		return id
	}
	return newID(last.Add(time.Second))
}

func readRecords(dir string) ([]Record, error) {
	entries, err := os.ReadDir(filepath.Join(dir, recordsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	var records []Record
	for _, entry := range entries {
		if _, ok := trimExt(entry.Name(), jsonExt); !ok {
			continue
		}
		record, err := readRecord(filepath.Join(dir, recordsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b Record) int { return strings.Compare(a.ID, b.ID) })
	return records, nil
}

func readRecord(path string) (Record, error) {
	var record Record
	if err := readJSON(path, &record); err != nil {
		return Record{}, err
	}
	if record.Format != documentFormat {
		return Record{}, fmt.Errorf("snapshot: %s: format %d: %w", path, record.Format, ErrUnknownFormat)
	}
	if name, _ := trimExt(filepath.Base(path), jsonExt); record.ID != name {
		return Record{}, fmt.Errorf("snapshot: %s names snapshot %q: %w", path, record.ID, ErrCorrupt)
	}
	return record, nil
}

func recordPath(dir, id string) string {
	return filepath.Join(dir, recordsDir, id+jsonExt)
}

func writeRecord(dir string, record Record) error {
	record.Format = documentFormat
	return writeJSON(recordPath(dir, record.ID), record)
}

// Newest is the newest snapshot of a container, read from its record alone:
// no pack, no manifest and no lock, so a review screen may ask on every
// open. It is ErrNoSnapshot when there is none.
func Newest(loc Location) (Record, error) {
	entries, err := os.ReadDir(filepath.Join(loc.Dir(), recordsDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Record{}, fmt.Errorf("snapshot: %w", err)
	}
	var newest string
	for _, entry := range entries {
		if id, ok := trimExt(entry.Name(), jsonExt); ok && id > newest {
			newest = id
		}
	}
	if newest == "" {
		return Record{}, fmt.Errorf("snapshot: %s: %w", loc, ErrNoSnapshot)
	}
	return readRecord(recordPath(loc.Dir(), newest))
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- a file inside the snapshot store
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("snapshot: %s: %w: %w", path, ErrCorrupt, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot: %s: %w", path, err)
	}
	return pack.WriteFile(path, append(data, '\n'))
}
