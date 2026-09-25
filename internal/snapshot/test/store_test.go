package snapshot_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot"
)

// files lists every file under dir, relative to it.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return err
	}))
	return found
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(to, data, 0o600))
}

// TestOpenRemovesWhatACrashAtEachPublishStepLeft rebuilds, from a store
// that published a second snapshot, what the disk held had the capture
// died before each step of its publish, and checks that Open leaves the
// first snapshot exactly as it was.
func TestOpenRemovesWhatACrashAtEachPublishStepLeft(t *testing.T) {
	tests := []struct {
		name  string
		crash func(t *testing.T, dir, first, second string)
	}{
		{name: "before the change set", crash: func(t *testing.T, dir, first, second string) {
			removeFiles(t, dir, "records/"+second+".json", "changes/"+first+".."+second+".changes", "manifests/"+second+".manifest")
			writeFile(t, dir, "changes/"+first+".."+second+".changes.tmp")
		}},
		{name: "before the manifest", crash: func(t *testing.T, dir, first, second string) {
			removeFiles(t, dir, "records/"+second+".json", "manifests/"+second+".manifest")
			writeFile(t, dir, "manifests/"+second+".manifest.tmp")
		}},
		{name: "before the record", crash: func(t *testing.T, dir, first, second string) {
			removeFiles(t, dir, "records/"+second+".json")
			writeFile(t, dir, "records/"+second+".json.tmp")
		}},
		{name: "before the parent's manifest went", crash: func(t *testing.T, dir, first, second string) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, orders(20)...)
			first := f.take(snapshot.CaptureOptions{})
			dir := f.loc.Dir()
			saved := filepath.Join(t.TempDir(), "first.manifest")
			copyFile(t, filepath.Join(dir, "manifests", first.ID+".manifest"), saved)
			want := exported(t, f.open(), first.ID)
			f.put(order("o-0001", "c01", 7))
			second := f.take(snapshot.CaptureOptions{})
			copyFile(t, saved, filepath.Join(dir, "manifests", first.ID+".manifest"))
			tt.crash(t, dir, first.ID, second.ID)

			store := f.open()

			assert.Equal(t, want, exported(t, store, first.ID))
			for _, file := range files(t, dir) {
				assert.False(t, strings.HasSuffix(file, ".tmp"), "debris %s", file)
			}
			assert.Len(t, filesIn(t, dir, "manifests"), 1, "the head's manifest alone")
			_, err := store.Verify(snapshot.VerifyOptions{Deep: true}, f.clock.Now())
			require.NoError(t, err)
		})
	}
}

func removeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, os.Remove(filepath.Join(dir, name)))
	}
}

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("partial"), 0o600))
}

func filesIn(t *testing.T, dir, sub string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, sub))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAStaleLockIsTakenOver(t *testing.T) {
	f := newFixture(t, orders(3)...)
	f.take(snapshot.CaptureOptions{})
	host, err := os.Hostname()
	require.NoError(t, err)
	stale, err := json.Marshal(map[string]any{"pid": deadPID(t), "host": host, "started": time.Now()})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.loc.Dir(), "lock"), stale, 0o600))

	f.take(snapshot.CaptureOptions{})

	assert.Len(t, f.open().Snapshots(), 2)
}

// deadPID is a process id no process has: the highest a Linux kernel will
// ever hand out, plus one.
func deadPID(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("process liveness is checked by handle on Windows")
	}
	return 1 << 22
}

// TestTwoTakersOfAStaleLockNeverBothHoldIt starts takers together on a
// store whose lock file a dead process left, each holding what it takes
// until every one has tried. The stores are opened first, since opening
// one takes the lock for a moment to tidy.
func TestTwoTakersOfAStaleLockNeverBothHoldIt(t *testing.T) {
	const takers = 8
	f := newFixture(t, orders(3)...)
	f.take(snapshot.CaptureOptions{})
	for round := range 30 {
		stale, err := json.Marshal(map[string]any{"pid": 1 << 22, "host": "here", "started": time.Now()})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(f.loc.Dir(), "lock"), stale, 0o600))
		var won atomic.Int32
		var tried, release sync.WaitGroup
		tried.Add(takers)
		release.Add(1)
		var done sync.WaitGroup
		for range takers {
			store := f.open()
			done.Add(1)
			go func() {
				defer done.Done()
				capture, err := store.Begin(f.source(), snapshot.CaptureOptions{})
				if err == nil {
					won.Add(1)
				} else if !errors.Is(err, snapshot.ErrLocked) {
					t.Error(err)
				}
				tried.Done()
				release.Wait()
				if capture != nil {
					assert.NoError(t, capture.Abort())
				}
			}()
		}
		tried.Wait()
		assert.Equal(t, int32(1), won.Load(), "round %d", round)
		release.Done()
		done.Wait()
	}
}

func TestAReleaseNeverFreesAnotherHoldersLock(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err)
	require.NoError(t, first.Abort())
	second, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Abort() })

	require.NoError(t, first.Abort(), "a second abort does nothing")

	_, err = f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.ErrorIs(t, err, snapshot.ErrLocked)
}

func TestAnUnknownFormatIsRefused(t *testing.T) {
	f := newFixture(t, orders(3)...)
	f.take(snapshot.CaptureOptions{})
	require.NoError(t, os.WriteFile(filepath.Join(f.loc.Dir(), "FORMAT"), []byte("alchemist-snapshot-store 2\n"), 0o600))

	_, err := snapshot.Open(f.loc)

	require.ErrorIs(t, err, snapshot.ErrUnknownFormat)
}

func TestEverythingIsPrivateToItsOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	f := newFixture(t, orders(3)...)
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0001", "c01", 7))
	f.take(snapshot.CaptureOptions{})

	require.NoError(t, filepath.WalkDir(f.loc.AccountDir(), func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		info, err := entry.Info()
		require.NoError(t, err)
		want := fs.FileMode(0o600)
		if entry.IsDir() {
			want = 0o700
		}
		assert.Equal(t, want, info.Mode().Perm(), path)
		return nil
	}))
}

func TestNewestReadsTheNewestRecordAlone(t *testing.T) {
	f := newFixture(t, orders(3)...)
	_, err := snapshot.Newest(f.loc)
	require.ErrorIs(t, err, snapshot.ErrNoSnapshot, "a store never written")
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0001", "c01", 7))
	second := f.take(snapshot.CaptureOptions{})
	require.NoError(t, os.RemoveAll(filepath.Join(f.loc.Dir(), "packs")))
	require.NoError(t, os.WriteFile(filepath.Join(f.loc.Dir(), "lock"), []byte(`{"pid":1,"host":"elsewhere"}`), 0o600))

	newest, err := snapshot.Newest(f.loc)

	require.NoError(t, err)
	assert.Equal(t, second.ID, newest.ID)
}

func TestNewestOfAStoreWithNoRecordsIsNoSnapshot(t *testing.T) {
	f := newFixture(t, orders(3)...)
	record := f.take(snapshot.CaptureOptions{})
	require.NoError(t, os.Remove(filepath.Join(f.loc.Dir(), "records", record.ID+".json")))

	_, err := snapshot.Newest(f.loc)

	require.ErrorIs(t, err, snapshot.ErrNoSnapshot)
}

func TestResolveAcceptsIdsPrefixesAndNames(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first := f.take(snapshot.CaptureOptions{})
	f.clock.advance(24 * time.Hour)
	second := f.take(snapshot.CaptureOptions{})
	store := f.open()
	tests := []struct {
		ref     string
		want    string
		wantErr error
	}{
		{ref: "latest", want: second.ID},
		{ref: "previous", want: first.ID},
		{ref: first.ID, want: first.ID},
		{ref: "20260920", want: second.ID},
		{ref: "2026", wantErr: snapshot.ErrNoSnapshot},
		{ref: "1999", wantErr: snapshot.ErrNoSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got, err := store.Resolve(tt.ref)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}

func TestStoresListsEveryStoreOnDiskByItsRealNames(t *testing.T) {
	f := newFixture(t, orders(3)...)
	f.loc.Container = "Orders.v2 (old)"
	f.take(snapshot.CaptureOptions{})

	locations, err := snapshot.Stores(f.loc.Root)

	require.NoError(t, err)
	require.Len(t, locations, 1)
	assert.Equal(t, f.loc, locations[0])
}

func TestSegmentsKeepNamesThatDifferOnlyInCaseApart(t *testing.T) {
	assert.NotEqual(t, strings.ToLower(snapshot.Segment("Orders")), strings.ToLower(snapshot.Segment("orders")))
	assert.True(t, strings.HasPrefix(snapshot.Segment("a b.c"), "a_b_c~"))
}

// TestOneProcessNeverLocksAStoreTwice holds the store with one capture and
// asks for it again from this process, by two spellings of its path: the
// second is refused whatever the file system's advisory locks allow.
func TestOneProcessNeverLocksAStoreTwice(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err)
	t.Chdir(f.loc.Root)
	relative := f.loc
	relative.Root = "."

	_, err = snapshot.Open(relative)
	require.NoError(t, err, "opening tidies only when it can lock, and leaves a held store alone")
	store, err := snapshot.Open(relative)
	require.NoError(t, err)
	_, err = store.Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))

	require.ErrorIs(t, err, snapshot.ErrLocked)
	assert.Contains(t, err.Error(), "held by this process")
	require.NoError(t, first.Abort())
	second, err := store.Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err, "a release lets this process lock it again")
	require.NoError(t, second.Abort())
}
