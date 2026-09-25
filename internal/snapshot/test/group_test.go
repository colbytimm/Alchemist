package snapshot_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

func (f *fixture) groupSource(containers ...string) snapshot.GroupSource {
	source := snapshot.GroupSource{Throughput: f.conn.(adapter.ThroughputEditor)}
	for _, name := range containers {
		container := f.source()
		container.Container = []string{"sales", name}
		source.Containers = append(source.Containers, container)
	}
	return source
}

func (f *fixture) takeGroup(source snapshot.GroupSource) snapshot.Group {
	f.t.Helper()
	f.clock.advance(time.Minute)
	database := f.loc
	database.Container = ""
	capture, err := snapshot.BeginGroup(database, source, snapshot.CaptureOptions{Clock: f.clock.Now, Note: "nightly"})
	require.NoError(f.t, err)
	for {
		progress, err := capture.Next(context.Background())
		require.NoError(f.t, err)
		if progress.Done {
			return progress.Group
		}
	}
}

func TestADatabaseSnapshotTakesEveryContainerInTurn(t *testing.T) {
	f := newFixture(t, orders(12)...)

	group := f.takeGroup(f.groupSource("orders", "customers"))

	require.Len(t, group.Containers, 2)
	for _, member := range group.Containers {
		assert.NotEmpty(t, member.Snapshot, member.Container)
		assert.Empty(t, member.Error)
	}
	groups, err := snapshot.Groups(f.loc)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, group.ID, groups[0].ID)
	assert.Equal(t, "nightly", groups[0].Note)
	newest, err := snapshot.Newest(f.loc)
	require.NoError(t, err)
	assert.Equal(t, group.ID, newest.Group)
}

func TestAContainerThatFailsIsRecordedAndTheRestStand(t *testing.T) {
	f := newFixture(t, orders(4)...)

	group := f.takeGroup(f.groupSource("missing", "orders"))

	require.Len(t, group.Containers, 2)
	assert.NotEmpty(t, group.Containers[0].Error)
	assert.NotEmpty(t, group.Containers[1].Snapshot)
}

func TestAGroupDiffListsEachContainer(t *testing.T) {
	f := newFixture(t, orders(6)...)
	first := f.takeGroup(f.groupSource("orders", "customers"))
	f.put(order("o-new", "c01", 1))
	second := f.takeGroup(f.groupSource("orders"))

	changes, err := snapshot.DiffGroups(f.loc, first, second)

	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.Equal(t, "customers", changes[0].Container)
	assert.Equal(t, snapshot.ContainerDisappeared, changes[0].State)
	assert.Equal(t, snapshot.ContainerCompared, changes[1].State)
	assert.Equal(t, 1, changes[1].Added)
}
