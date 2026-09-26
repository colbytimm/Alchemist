package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/colbytimm/alchemist/internal/snapshot/pack"
)

// Usage is what a store costs, beside what it would cost as exports: the
// efficiency a snapshot claims, made visible.
type Usage struct {
	Snapshots int
	// Items is how many items the newest snapshot holds.
	Items int64
	// LogicalBytes is the sum over snapshots of their canonical bodies: what
	// one export per snapshot would weigh.
	LogicalBytes int64
	Packs        int64
	Indexes      int64
	Manifests    int64
	Changes      int64
	Other        int64
}

func (u Usage) OnDisk() int64 {
	return u.Packs + u.Indexes + u.Manifests + u.Changes + u.Other
}

// Ratio is how many times smaller the store is than its exports; zero for
// an empty store.
func (u Usage) Ratio() float64 {
	if u.OnDisk() == 0 {
		return 0
	}
	return float64(u.LogicalBytes) / float64(u.OnDisk())
}

func (u Usage) Add(other Usage) Usage {
	return Usage{
		Snapshots:    u.Snapshots + other.Snapshots,
		Items:        u.Items + other.Items,
		LogicalBytes: u.LogicalBytes + other.LogicalBytes,
		Packs:        u.Packs + other.Packs,
		Indexes:      u.Indexes + other.Indexes,
		Manifests:    u.Manifests + other.Manifests,
		Changes:      u.Changes + other.Changes,
		Other:        u.Other + other.Other,
	}
}

func (s *Store) Usage() (Usage, error) {
	u := Usage{Snapshots: len(s.records)}
	for _, record := range s.records {
		u.LogicalBytes += record.LogicalBytes
	}
	if head, ok := s.head(); ok {
		u.Items = head.Items
	}
	err := filepath.WalkDir(s.loc.Dir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		*u.bucket(entry.Name()) += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Usage{}, fmt.Errorf("snapshot: %s: %w", s.loc, err)
	}
	return u, nil
}

func (u *Usage) bucket(name string) *int64 {
	switch {
	case strings.HasSuffix(name, pack.PackExt):
		return &u.Packs
	case strings.HasSuffix(name, pack.IndexExt):
		return &u.Indexes
	case strings.HasSuffix(name, manifestExt):
		return &u.Manifests
	case strings.HasSuffix(name, changesExt):
		return &u.Changes
	}
	return &u.Other
}

// AccountUsage totals every store of account, for a profile being removed.
func AccountUsage(root, account string) (Usage, error) {
	locations, err := Stores(root)
	if err != nil {
		return Usage{}, err
	}
	var total Usage
	for _, loc := range locations {
		if loc.Account != account {
			continue
		}
		store, err := Open(loc)
		if err != nil {
			return Usage{}, err
		}
		u, err := store.Usage()
		if err != nil {
			return Usage{}, err
		}
		total = total.Add(u)
	}
	return total, nil
}
