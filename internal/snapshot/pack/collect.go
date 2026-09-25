package pack

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// maxPacks is how many packs a collection leaves before it merges the
// smallest, so a store of many small captures does not become many small
// files.
const maxPacks = 64

// Collected is what one collection did.
type Collected struct {
	Removed   int
	Rewritten int
	Reclaimed int64
}

type packUsage struct {
	name  string
	total int
	live  int
	bytes int64
}

// Collect keeps every body live names and reclaims the rest. A pack with
// nothing live is deleted; one less than half live is rewritten with its
// live bodies alone, into packs named for prefix, and then deleted; one at
// least half live stays as it is, which bounds the waste at 2× without
// rewriting 64 MB to reclaim a kilobyte. Past maxPacks, the smallest are
// merged the same way. The caller must hold the store's lock.
func Collect(dir, prefix string, live map[Hash]bool) (Collected, error) {
	usages, err := measure(dir, live)
	if err != nil {
		return Collected{}, err
	}
	empty, sparse, kept := classify(usages)
	sparse = withSmallestMerged(sparse, kept)
	var done Collected
	for _, u := range empty {
		if err := removePack(dir, u.name); err != nil {
			return done, err
		}
		done.Removed++
		done.Reclaimed += u.bytes
	}
	if len(sparse) == 0 {
		return done, nil
	}
	written, err := rewrite(dir, prefix, sparse, live)
	if err != nil {
		return done, err
	}
	for _, u := range sparse {
		if err := removePack(dir, u.name); err != nil {
			return done, err
		}
		done.Rewritten++
		done.Reclaimed += u.bytes
	}
	done.Reclaimed -= written
	return done, nil
}

func measure(dir string, live map[Hash]bool) ([]packUsage, error) {
	names, err := Names(dir)
	if err != nil {
		return nil, err
	}
	livePrefixes := make(map[prefix]bool, len(live))
	for h := range live {
		livePrefixes[prefixOf(h)] = true
	}
	usages := make([]packUsage, 0, len(names))
	for _, name := range names {
		x, err := indexOrRebuild(dir, name)
		if err != nil {
			return nil, err
		}
		u := packUsage{name: name, total: len(x.entries), bytes: fileSize(dir, name+PackExt) + fileSize(dir, name+IndexExt)}
		for _, entry := range x.entries {
			if livePrefixes[entry.prefix] {
				u.live++
			}
		}
		usages = append(usages, u)
	}
	return usages, nil
}

// indexOrRebuild reads a pack's index, writing it first when a crash left
// the pack without one: a pack must never be judged dead for want of it.
func indexOrRebuild(dir, name string) (index, error) {
	path := filepath.Join(dir, name+IndexExt)
	x, err := readIndex(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return x, err
	}
	if err := RebuildIndex(dir, name); err != nil {
		return index{}, err
	}
	return readIndex(path)
}

func classify(usages []packUsage) (empty, sparse, kept []packUsage) {
	for _, u := range usages {
		switch {
		case u.live == 0:
			empty = append(empty, u)
		case 2*u.live < u.total:
			sparse = append(sparse, u)
		default:
			kept = append(kept, u)
		}
	}
	return empty, sparse, kept
}

// withSmallestMerged adds the smallest kept packs to the rewrite until what
// is left, and the pack the rewrite makes, fit in maxPacks.
func withSmallestMerged(sparse, kept []packUsage) []packUsage {
	made := 0
	if len(sparse) > 0 {
		made = 1
	}
	if len(kept)+made <= maxPacks {
		return sparse
	}
	kept = slices.Clone(kept)
	slices.SortFunc(kept, func(a, b packUsage) int { return cmp.Compare(a.bytes, b.bytes) })
	excess := len(kept) + 1 - maxPacks
	if made == 0 {
		excess = max(excess, 2) // a merge of one pack would only copy it
	}
	return append(sparse, kept[:excess]...)
}

func rewrite(dir, prefix string, packs []packUsage, live map[Hash]bool) (int64, error) {
	writer, err := NewWriter(dir, prefix)
	if err != nil {
		return 0, err
	}
	for _, u := range packs {
		err := Walk(dir, u.name, func(body Body) error {
			if !live[body.Hash] || writer.Has(body.Hash) {
				return nil
			}
			return writer.Add(body.Hash, body.Data)
		})
		if err != nil {
			writer.Abort()
			return 0, err
		}
	}
	if err := writer.Close(); err != nil {
		writer.Abort()
		return 0, err
	}
	return writer.Stored(), nil
}

// removePack deletes the index before the pack, for the reason Writer
// publishes them the other way round.
func removePack(dir, name string) error {
	for _, file := range []string{name + IndexExt, name + PackExt} {
		if err := os.Remove(filepath.Join(dir, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("pack: %w", err)
		}
	}
	return nil
}

func fileSize(dir, file string) int64 {
	info, err := os.Stat(filepath.Join(dir, file))
	if err != nil {
		return 0
	}
	return info.Size()
}
