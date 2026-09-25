package snapshot_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot"
)

// history takes five snapshots of a container that changes between each,
// and returns their ids with the contents each export held.
func history(t *testing.T, f *fixture) ([]string, map[string][]string) {
	t.Helper()
	var ids []string
	contents := map[string][]string{}
	for i := range 5 {
		if i > 0 {
			f.put(order(fmt.Sprintf("o-%04d", i), "c01", float64(100+i)), order(fmt.Sprintf("n-%d", i), "c02", 1))
			f.remove(fmt.Sprintf("c%02d", (i+10)%7), fmt.Sprintf("o-%04d", i+10))
		}
		record := f.take(snapshot.CaptureOptions{})
		ids = append(ids, record.ID)
		contents[record.ID] = exported(t, f.open(), record.ID)
	}
	return ids, contents
}

func TestDeletingASnapshotLeavesEveryOtherAsItWas(t *testing.T) {
	for _, position := range []int{0, 2, 4} {
		t.Run(fmt.Sprintf("snapshot %d of 5", position+1), func(t *testing.T) {
			f := newFixture(t, orders(30)...)
			ids, contents := history(t, f)

			require.NoError(t, f.open().Delete(ids[position], f.clock.Now()))

			store := f.open()
			require.Len(t, store.Snapshots(), 4)
			for i, id := range ids {
				if i != position {
					assert.Equal(t, contents[id], exported(t, store, id), "snapshot %d", i+1)
				}
			}
			_, err := store.Verify(snapshot.VerifyOptions{Deep: true}, f.clock.Now())
			require.NoError(t, err)
		})
	}
}

func TestDeletingTheOnlySnapshotEmptiesTheStore(t *testing.T) {
	f := newFixture(t, orders(5)...)
	record := f.take(snapshot.CaptureOptions{})

	require.NoError(t, f.open().Delete(record.ID, f.clock.Now()))

	assert.Empty(t, f.open().Snapshots())
	u, err := f.open().Usage()
	require.NoError(t, err)
	assert.Zero(t, u.Packs, "garbage collection took every body")
	f.take(snapshot.CaptureOptions{})
}

// TestGarbageCollectionNeverTakesAReachableBody captures and deletes at
// random, and after every step reads every surviving snapshot in full.
func TestGarbageCollectionNeverTakesAReachableBody(t *testing.T) {
	for seed := range uint64(4) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			random := rand.New(rand.NewPCG(seed, 19))
			f := newFixture(t, orders(40)...)
			want := map[string][]string{}
			for range 14 {
				mutate(f, random)
				if records := f.open().Snapshots(); len(records) > 1 && random.IntN(3) == 0 {
					victim := records[random.IntN(len(records))].ID
					require.NoError(t, f.open().Delete(victim, f.clock.Now()))
					delete(want, victim)
				} else {
					record := f.take(snapshot.CaptureOptions{Full: random.IntN(4) == 0})
					want[record.ID] = f.held()
				}
				store := f.open()
				for id, held := range want {
					require.Equal(t, held, exported(t, store, id), "snapshot %s", id)
				}
			}
			_, err := f.open().Verify(snapshot.VerifyOptions{Deep: true}, f.clock.Now())
			require.NoError(t, err)
		})
	}
}

// mutate rewrites, adds and deletes a few items at random.
func mutate(f *fixture, random *rand.Rand) {
	for range random.IntN(6) {
		id := fmt.Sprintf("o-%04d", random.IntN(60))
		customer := fmt.Sprintf("c%02d", random.IntN(60)%7)
		if random.IntN(4) == 0 {
			_ = f.mock.DeleteItem(ordersPath, id, keyOf(customer)) // absent is as good as deleted
			continue
		}
		f.put(order(id, customer, float64(random.IntN(1000))))
	}
	f.clock.advance(time.Second)
}

func TestAPackUnderHalfLiveIsRewrittenAndOneOverIsNot(t *testing.T) {
	tests := []struct {
		name        string
		rewrite     int
		wantRewrite bool
	}{
		{name: "most bodies gone", rewrite: 80, wantRewrite: true},
		{name: "few bodies gone", rewrite: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, orders(100)...)
			first := f.take(snapshot.CaptureOptions{})
			for i := range tt.rewrite {
				f.put(order(fmt.Sprintf("o-%04d", i), fmt.Sprintf("c%02d", i%7), float64(1000+i)))
			}
			f.take(snapshot.CaptureOptions{})
			firstPacks := packsNamedFor(t, f, first.ID)

			require.NoError(t, f.open().Delete(first.ID, f.clock.Now()))

			assert.Equal(t, tt.wantRewrite, len(packsNamedFor(t, f, first.ID)) == 0, "packs of %v", firstPacks)
		})
	}
}

func packsNamedFor(t *testing.T, f *fixture, capture string) []string {
	t.Helper()
	var named []string
	for _, name := range filesIn(t, f.loc.Dir(), "packs") {
		if strings.HasPrefix(name, capture) {
			named = append(named, name)
		}
	}
	return named
}

func TestPruneKeepsTheUnionOfItsRulesAndTheHead(t *testing.T) {
	f := newFixture(t, orders(5)...)
	var ids []string
	for day := range 10 {
		if day > 0 {
			f.clock.advance(24*time.Hour - 2*time.Minute)
		}
		for range 2 {
			f.put(order("o-0001", "c01", float64(day)))
			ids = append(ids, f.take(snapshot.CaptureOptions{}).ID)
		}
	}
	store := f.open()
	tests := []struct {
		name   string
		policy snapshot.Policy
		want   []string
	}{
		{name: "the last three", policy: snapshot.Policy{KeepLast: 3}, want: ids[17:]},
		{name: "none but the head", policy: snapshot.Policy{}, want: ids[19:]},
		{name: "the newest of each of the last three days", policy: snapshot.Policy{KeepDaily: 3},
			want: []string{ids[15], ids[17], ids[19]}},
		{name: "the union", policy: snapshot.Policy{KeepLast: 2, KeepDaily: 2}, want: []string{ids[17], ids[18], ids[19]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pruned := store.Pruned(tt.policy, f.clock.Now())

			var kept []string
			for _, id := range ids {
				if !containsRecord(pruned, id) {
					kept = append(kept, id)
				}
			}
			assert.Equal(t, tt.want, kept)
		})
	}
}

func containsRecord(records []snapshot.Record, id string) bool {
	for _, record := range records {
		if record.ID == id {
			return true
		}
	}
	return false
}

func TestPruneDeletesAndCollects(t *testing.T) {
	f := newFixture(t, orders(20)...)
	ids, contents := history(t, f)

	pruned, err := f.open().Prune(snapshot.Policy{KeepLast: 2}, f.clock.Now())

	require.NoError(t, err)
	assert.Len(t, pruned, 3)
	store := f.open()
	assert.Len(t, store.Snapshots(), 2)
	for _, id := range ids[3:] {
		assert.Equal(t, contents[id], exported(t, store, id))
	}
}

func TestVerifyFindsWhatHasRotted(t *testing.T) {
	tests := []struct {
		name string
		rot  func(t *testing.T, dir string)
		deep bool
	}{
		{name: "a record gone from the middle", rot: func(t *testing.T, dir string) {
			records := filesIn(t, dir, "records")
			require.NoError(t, os.Remove(filepath.Join(dir, "records", records[1])))
		}},
		{name: "a change set truncated", rot: func(t *testing.T, dir string) {
			changes := filesIn(t, dir, "changes")
			truncate(t, filepath.Join(dir, "changes", changes[0]))
		}},
		{name: "a manifest truncated", rot: func(t *testing.T, dir string) {
			truncate(t, filepath.Join(dir, "manifests", filesIn(t, dir, "manifests")[0]))
		}},
		{name: "a pack gone", rot: func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(firstPack(t, dir)))
		}},
		{name: "a byte of a pack flipped", rot: func(t *testing.T, dir string) {
			flip(t, firstPack(t, dir))
		}, deep: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, orders(20)...)
			history(t, f)
			tt.rot(t, f.loc.Dir())

			store, err := snapshot.Open(f.loc)
			if err == nil {
				_, err = store.Verify(snapshot.VerifyOptions{Deep: tt.deep}, f.clock.Now())
			}

			require.Error(t, err)
			assert.True(t, isRefused(err), "%v", err)
		})
	}
}

func firstPack(t *testing.T, dir string) string {
	t.Helper()
	for _, name := range filesIn(t, dir, "packs") {
		if strings.HasSuffix(name, ".pack") {
			return filepath.Join(dir, "packs", name)
		}
	}
	require.Fail(t, "no pack")
	return ""
}

func truncate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(path, info.Size()-3))
}

func flip(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-5] ^= 0x40
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func TestVerifyRebuildsIndexes(t *testing.T) {
	f := newFixture(t, orders(20)...)
	_, contents := history(t, f)
	for _, name := range filesIn(t, f.loc.Dir(), "packs") {
		if strings.HasSuffix(name, ".idx") {
			require.NoError(t, os.Remove(filepath.Join(f.loc.Dir(), "packs", name)))
		}
	}

	_, err := f.open().Verify(snapshot.VerifyOptions{RebuildIndex: true, Deep: true}, f.clock.Now())

	require.NoError(t, err)
	store := f.open()
	for id, want := range contents {
		assert.Equal(t, want, exported(t, store, id))
	}
}
