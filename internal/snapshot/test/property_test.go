package snapshot_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/snapshot"
)

// TestASnapshotRestoresExactlyWhatWasHeldAndIncrementalEqualsFull is the
// restore property: for random items and random writes, the export of
// every snapshot is exactly what the container held when it was taken,
// and each incremental capture publishes the manifest a full one would.
func TestASnapshotRestoresExactlyWhatWasHeldAndIncrementalEqualsFull(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			random := rand.New(rand.NewPCG(seed, 7))
			f := newFixture(t, orders(random.IntN(40))...)
			full := *f
			full.loc.Root = t.TempDir()
			held := map[string][]string{}
			for range 8 {
				mutate(f, random)
				incremental := f.take(snapshot.CaptureOptions{})
				held[incremental.ID] = f.held()
				reference := takeFrom(t, full.open(), full.source(), f.withClock(snapshot.CaptureOptions{Full: true}))

				got, err := f.open().Contents(incremental.ID)
				require.NoError(t, err)
				want, err := full.open().Contents(reference.ID)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
			store := f.open()
			for id, want := range held {
				require.Equal(t, want, exported(t, store, id), "snapshot %s", id)
			}
		})
	}
}
