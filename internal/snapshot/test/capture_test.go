package snapshot_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

func TestAFirstSnapshotHoldsEveryItemAsItWas(t *testing.T) {
	f := newFixture(t, orders(30)...)

	record := f.take(snapshot.CaptureOptions{Note: "nightly"})

	assert.Equal(t, snapshot.ModeFull, record.Mode)
	assert.Equal(t, int64(30), record.Items)
	assert.Equal(t, "nightly", record.Note)
	assert.Empty(t, record.Parent)
	assert.NotEmpty(t, record.Definition)
	assert.Positive(t, record.RequestCharge)
	assert.Positive(t, record.LogicalBytes)
	assert.Equal(t, "20260919T060100Z", record.ID)
	assert.Equal(t, f.held(), exported(t, f.open(), record.ID))
}

func TestAnEmptyContainerSnapshotsAsEmpty(t *testing.T) {
	f := newFixture(t)

	record := f.take(snapshot.CaptureOptions{})

	assert.Zero(t, record.Items)
	assert.Empty(t, exported(t, f.open(), record.ID))
}

func TestALaterSnapshotIsIncrementalAndRecordsOnlyWhatChanged(t *testing.T) {
	f := newFixture(t, orders(30)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(order("o-0003", "c03", 99), order("o-new", "c01", 1))
	f.remove("c05", "o-0005")

	second := f.take(snapshot.CaptureOptions{})

	assert.Equal(t, snapshot.ModeIncremental, second.Mode)
	assert.Equal(t, first.ID, second.Parent)
	assert.Equal(t, [3]int64{1, 1, 1}, [3]int64{second.Added, second.Removed, second.Modified})
	assert.Equal(t, int64(30), second.Items)
	assert.Equal(t, f.held(), exported(t, f.open(), second.ID))
}

func TestFullForcesAScanOfEveryItem(t *testing.T) {
	f := newFixture(t, orders(10)...)
	f.take(snapshot.CaptureOptions{})

	record := f.take(snapshot.CaptureOptions{Full: true})

	assert.Equal(t, snapshot.ModeFull, record.Mode)
	assert.Zero(t, record.Changes())
}

func TestTwoWritesInOneSecondAreBothSeen(t *testing.T) {
	f := newFixture(t, orders(5)...)
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0001", "c01", 50))
	f.take(snapshot.CaptureOptions{})
	require.NoError(t, f.mock.PutItem(ordersPath, json.RawMessage(order("o-0001", "c01", 51))), "same second as the write before")

	third := f.take(snapshot.CaptureOptions{})

	assert.Equal(t, int64(1), third.Modified)
	assert.Equal(t, f.held(), exported(t, f.open(), third.ID))
}

func TestANoOpReplaceIsNoChange(t *testing.T) {
	f := newFixture(t, orders(5)...)
	f.take(snapshot.CaptureOptions{})
	f.put(order("o-0002", "c02", 2))

	record := f.take(snapshot.CaptureOptions{})

	assert.Zero(t, record.Changes())
	d, err := f.open().Diff(record.Parent, record.ID)
	require.NoError(t, err)
	assert.Empty(t, d.Items)
}

func TestAnUnchangedContainerCostsAlmostNothing(t *testing.T) {
	f := newFixture(t, orders(200)...)
	f.take(snapshot.CaptureOptions{})
	before, err := f.open().Usage()
	require.NoError(t, err)

	f.take(snapshot.CaptureOptions{})

	after, err := f.open().Usage()
	require.NoError(t, err)
	assert.Less(t, after.OnDisk()-before.OnDisk(), int64(4096))
}

func TestASecondBeginWaitsForTheFirst(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err)

	_, err = f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{}))

	require.ErrorIs(t, err, snapshot.ErrLocked)
	require.NoError(t, first.Abort())
	f.take(snapshot.CaptureOptions{})
}

func TestAnAbortedCaptureListsNothingAndReleasesTheLock(t *testing.T) {
	for _, steps := range []int{0, 1, 3, 6} {
		t.Run(fmt.Sprintf("after %d steps", steps), func(t *testing.T) {
			f := newFixture(t, orders(30)...)
			f.take(snapshot.CaptureOptions{})
			f.put(order("o-0001", "c01", 7))
			capture, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{Full: true}))
			require.NoError(t, err)
			for range steps {
				_, err := capture.Next(context.Background())
				require.NoError(t, err)
			}

			require.NoError(t, capture.Abort())

			assert.Len(t, f.open().Snapshots(), 1)
			f.take(snapshot.CaptureOptions{})
			assert.Len(t, f.open().Snapshots(), 2)
		})
	}
}

func TestALargerContainerThanTheLimitIsRefusedBeforeAnyItemIsRead(t *testing.T) {
	f := newFixture(t, orders(30)...)
	capture, err := f.open().Begin(f.source(), f.withClock(snapshot.CaptureOptions{MaxItems: 10}))
	require.NoError(t, err)

	progress, err := capture.Next(context.Background())

	require.ErrorIs(t, err, snapshot.ErrTooManyItems)
	assert.Contains(t, err.Error(), "snapshot_max_items")
	assert.Zero(t, progress.RequestCharge)
	require.NoError(t, capture.Abort())
}

// throttledScanner fails the first page reads it is asked for as throttled,
// then serves them.
type throttledScanner struct {
	inner     adapter.ItemScanner
	throttles int
}

func (s *throttledScanner) ScanItems(ctx context.Context, request adapter.ScanRequest) (adapter.ItemScan, error) {
	scan, err := s.inner.ScanItems(ctx, request)
	return &throttledScan{ItemScan: scan, scanner: s}, err
}

type throttledScan struct {
	adapter.ItemScan
	scanner *throttledScanner
}

func (s *throttledScan) NextPage(ctx context.Context) (adapter.ItemPage, error) {
	if s.scanner.throttles > 0 {
		s.scanner.throttles--
		return adapter.ItemPage{}, &adapter.ThrottledError{RetryAfter: time.Second, Err: errors.New("429 Too Many Requests")}
	}
	return s.ItemScan.NextPage(ctx)
}

func TestAThrottledPageCanBeReadAgain(t *testing.T) {
	f := newFixture(t, orders(20)...)
	source := f.source()
	source.Items = &throttledScanner{inner: source.Items, throttles: 3}
	capture, err := f.open().Begin(source, f.withClock(snapshot.CaptureOptions{}))
	require.NoError(t, err)

	throttled := 0
	for {
		progress, err := capture.Next(context.Background())
		var refusal *adapter.ThrottledError
		if errors.As(err, &refusal) {
			throttled++
			continue
		}
		require.NoError(t, err)
		if progress.Done {
			break
		}
	}

	assert.Equal(t, 3, throttled)
	latest, err := f.open().Resolve(snapshot.RefLatest)
	require.NoError(t, err)
	assert.Equal(t, f.held(), exported(t, f.open(), latest.ID))
}

func TestASnapshotWithoutADefinitionReaderSaysSo(t *testing.T) {
	f := newFixture(t, orders(3)...)
	source := f.source()
	source.Definitions, source.Throughput, source.PartitionKeys = nil, nil, []string{"/customerId"}
	store := f.open()
	first := takeFrom(t, store, source, f.withClock(snapshot.CaptureOptions{}))
	f.clock.advance(time.Minute)
	second := takeFrom(t, f.open(), source, f.withClock(snapshot.CaptureOptions{}))

	d, err := f.open().Diff(first.ID, second.ID)

	require.NoError(t, err)
	assert.Empty(t, second.Definition)
	assert.Equal(t, snapshot.DefinitionNotCaptured, d.Definition.State)
	assert.Equal(t, "definition not captured", d.Definition.Summary())
}

func TestADifferentThroughputChangesTheDefinitionAndASizeAloneDoesNot(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first := f.take(snapshot.CaptureOptions{})
	f.put(order("o-extra", "c01", 1))
	second := f.take(snapshot.CaptureOptions{})
	require.NoError(t, f.conn.(adapter.ThroughputEditor).SetThroughput(context.Background(), ordersPath,
		adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 1000}))
	third := f.take(snapshot.CaptureOptions{})
	store := f.open()

	assert.Equal(t, first.Definition, second.Definition, "an inserted item changes the size estimate only")
	assert.False(t, store.DefinitionChanged(second))
	assert.True(t, store.DefinitionChanged(third))
	d, err := store.Diff(second.ID, third.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"throughput: 400 RU/s manual → 1000 RU/s manual"}, d.Definition.Changes)
	assert.Equal(t, "definition: 1 setting", d.Definition.Summary())
}

func TestIdsNeverRepeatOrGoBackwards(t *testing.T) {
	f := newFixture(t, orders(3)...)
	first := takeFrom(t, f.open(), f.source(), f.withClock(snapshot.CaptureOptions{}))
	second := takeFrom(t, f.open(), f.source(), f.withClock(snapshot.CaptureOptions{}))
	f.clock.advance(-time.Hour)
	third := takeFrom(t, f.open(), f.source(), f.withClock(snapshot.CaptureOptions{}))

	assert.Less(t, first.ID, second.ID)
	assert.Less(t, second.ID, third.ID)
}

func TestAHierarchicalKeyOrdersItemsByEachComponentInTurn(t *testing.T) {
	c := newClock()
	a := mock.New(mock.WithClock(c.Now))
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	path := []string{"sales", "regional"}
	require.NoError(t, conn.(adapter.CatalogAdmin).CreateContainer(context.Background(),
		adapter.ContainerSpec{Database: "sales", Name: "regional", PartitionKeys: []string{"/region", "/city"}}))
	for _, item := range []string{
		`{"id":"3","region":"west","city":"Lima"}`, `{"id":"1","region":"east","city":"Oslo"}`,
		`{"id":"2","region":"east","city":"Lima"}`, `{"id":"4","region":"east"}`,
	} {
		require.NoError(t, a.PutItem(path, json.RawMessage(item)))
	}
	loc := snapshot.Location{Root: t.TempDir(), Account: "prod", Database: "sales", Container: "regional"}
	store, err := snapshot.Open(loc)
	require.NoError(t, err)
	source := snapshot.Source{Container: path, Items: conn.(adapter.ItemScanner), Definitions: conn.(adapter.DefinitionReader)}
	record := takeFrom(t, store, source, snapshot.CaptureOptions{Clock: c.Now})

	contents, err := store.Contents(record.ID)

	require.NoError(t, err)
	var order []string
	for _, key := range contents.SortedKeys() {
		identity, err := key.Identity()
		require.NoError(t, err)
		order = append(order, identity.PartitionKeyText()+" "+identity.ID)
	}
	assert.Equal(t, []string{"east/- 4", "east/Lima 2", "east/Oslo 1", "west/Lima 3"}, order,
		"an undefined component is empty, so it sorts first")
}
