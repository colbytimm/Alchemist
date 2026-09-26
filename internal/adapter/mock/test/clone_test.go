package mock_test

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

func definitions(t *testing.T, conn adapter.Connection) adapter.DefinitionReader {
	t.Helper()
	d, ok := conn.(adapter.DefinitionReader)
	require.True(t, ok, "a mock connection reads definitions")
	return d
}

func scanner(t *testing.T, conn adapter.Connection) adapter.ItemScanner {
	t.Helper()
	s, ok := conn.(adapter.ItemScanner)
	require.True(t, ok, "a mock connection scans")
	return s
}

func openSink(t *testing.T, conn adapter.Connection, path []string) adapter.ItemSink {
	t.Helper()
	w, ok := conn.(adapter.ItemWriter)
	require.True(t, ok, "a mock connection writes items")
	s, err := w.OpenItemSink(context.Background(), path)
	require.NoError(t, err)
	return s
}

func connectTo(t *testing.T, a *mock.Adapter) adapter.Connection {
	t.Helper()
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	return conn
}

func TestADefinitionCarriesKeysPoliciesAndSize(t *testing.T) {
	conn := connect(t, mock.WithItemCount(ordersPath, 3))

	def, err := definitions(t, conn).ContainerDefinition(context.Background(), ordersPath, adapter.DefinitionFull)

	require.NoError(t, err)
	assert.Equal(t, []string{"/customerId"}, def.PartitionKeys)
	assert.Equal(t, mock.Name, def.Policies.Backend)
	assert.Contains(t, string(def.Policies.Raw), "vectorEmbeddingPolicy")
	assert.True(t, def.Size.Known)
	assert.Equal(t, int64(3), def.Size.Items)
	assert.Positive(t, def.Size.Bytes)
}

func TestAPortableDefinitionLeavesOutAccountBoundPolicies(t *testing.T) {
	def, err := definitions(t, connect(t)).ContainerDefinition(context.Background(), ordersPath, adapter.DefinitionPortable)

	require.NoError(t, err)
	assert.NotContains(t, string(def.Policies.Raw), "vectorEmbeddingPolicy")
	assert.Contains(t, string(def.Policies.Raw), "indexingPolicy")
}

func TestAnUnknownSizeSaysSo(t *testing.T) {
	def, err := definitions(t, connect(t, mock.WithUnknownSize())).ContainerDefinition(context.Background(), ordersPath, adapter.DefinitionFull)

	require.NoError(t, err)
	assert.False(t, def.Size.Known)
}

func TestADefinitionReadCanFail(t *testing.T) {
	_, err := definitions(t, connect(t, mock.WithError(mock.OpDefinition))).ContainerDefinition(context.Background(), ordersPath, adapter.DefinitionFull)

	var injected *mock.InjectedError
	require.ErrorAs(t, err, &injected)
}

func TestACreatedContainerKeepsItsPolicies(t *testing.T) {
	conn := connect(t)
	policies := adapter.PolicyDocument{Backend: mock.Name, Raw: json.RawMessage(`{"defaultTtl":60}`)}
	spec := adapter.ContainerSpec{Database: "sales", Name: "copy", PartitionKeys: []string{"/customerId"}, Policies: policies}

	require.NoError(t, admin(t, conn).CreateContainer(context.Background(), spec))

	def, err := definitions(t, conn).ContainerDefinition(context.Background(), []string{"sales", "copy"}, adapter.DefinitionFull)
	require.NoError(t, err)
	assert.JSONEq(t, `{"defaultTtl":60}`, string(def.Policies.Raw))
}

func TestCreateRefusesAnotherBackendsPolicies(t *testing.T) {
	spec := adapter.ContainerSpec{
		Database: "sales", Name: "copy", PartitionKeys: []string{"/customerId"},
		Policies: adapter.PolicyDocument{Backend: "cosmos", Raw: json.RawMessage(`{}`)},
	}

	err := admin(t, connect(t)).CreateContainer(context.Background(), spec)

	require.ErrorIs(t, err, adapter.ErrUnsupported)
}

func TestCreatingWhatExistsIsAConflict(t *testing.T) {
	conn := connect(t)

	dbErr := admin(t, conn).CreateDatabase(context.Background(), adapter.DatabaseSpec{Name: "sales"})
	containerErr := admin(t, conn).CreateContainer(context.Background(), adapter.ContainerSpec{Database: "sales", Name: "orders"})

	require.ErrorIs(t, dbErr, adapter.ErrAlreadyExists)
	require.ErrorIs(t, containerErr, adapter.ErrAlreadyExists)
}

func scanAll(t *testing.T, conn adapter.Connection, request adapter.ScanRequest) ([]adapter.ItemPage, []string) {
	t.Helper()
	scan, err := scanner(t, conn).ScanItems(context.Background(), request)
	require.NoError(t, err)
	var pages []adapter.ItemPage
	var all []json.RawMessage
	for scan.HasMore() {
		page, err := scan.NextPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, page)
		all = append(all, page.Items...)
	}
	require.NoError(t, scan.Close())
	return pages, ids(t, all)
}

func TestAScanPagesThroughEveryItem(t *testing.T) {
	conn := connect(t, mock.WithItemCount(ordersPath, 25))

	pages, got := scanAll(t, conn, adapter.ScanRequest{Container: ordersPath})

	require.Len(t, pages, 3)
	assert.Len(t, got, 25)
	assert.Equal(t, adapter.ScanPosition("10"), pages[0].Next)
	assert.Empty(t, pages[2].Next)
	assert.Positive(t, pages[0].RequestCharge)
}

func TestAScanResumesAfterThePageThatCarriedThePosition(t *testing.T) {
	conn := connect(t, mock.WithItemCount(ordersPath, 25))
	pages, all := scanAll(t, conn, adapter.ScanRequest{Container: ordersPath, PageSize: 7})

	_, resumed := scanAll(t, conn, adapter.ScanRequest{Container: ordersPath, PageSize: 7, From: pages[0].Next})

	assert.Equal(t, all[7:], resumed)
}

func TestAScanRefusesAPositionItDidNotIssue(t *testing.T) {
	_, err := scanner(t, connect(t)).ScanItems(context.Background(),
		adapter.ScanRequest{Container: ordersPath, From: "continuation"})

	require.Error(t, err)
}

func TestAScannedItemIsWholeAsStored(t *testing.T) {
	conn := connect(t, mock.WithItems(ordersPath, order("o1", "c01")))

	pages, _ := scanAll(t, conn, adapter.ScanRequest{Container: ordersPath})

	assert.Contains(t, string(pages[0].Items[0]), `"_etag"`)
}

func TestUpsertWritesByIDAndKey(t *testing.T) {
	a := mock.New()
	sink := openSink(t, connectTo(t, a), ordersPath)

	for _, body := range []json.RawMessage{order("o1", "c01"), order("o1", "c01"), order("o1", "c02")} {
		charge, err := sink.Upsert(context.Background(), body)
		require.NoError(t, err)
		assert.Positive(t, charge)
	}

	assert.Equal(t, []string{"o1", "o1"}, ids(t, a.Items(ordersPath)))
}

func TestUpsertRefusesAnItemWithNoKeyValue(t *testing.T) {
	sink := openSink(t, connect(t), ordersPath)

	_, err := sink.Upsert(context.Background(), json.RawMessage(`{"id":"o1"}`))

	require.ErrorIs(t, err, adapter.ErrItemRefused)
	require.ErrorIs(t, err, adapter.ErrNoPartitionKey)
}

func TestAThrottleLastsTheTimesItWasGiven(t *testing.T) {
	sink := openSink(t, connect(t, mock.WithThrottle("o1", 2, time.Second)), ordersPath)

	var errs []error
	for range 3 {
		_, err := sink.Upsert(context.Background(), order("o1", "c01"))
		errs = append(errs, err)
	}

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, errs[0], &throttled)
	assert.Equal(t, time.Second, throttled.RetryAfter)
	require.ErrorAs(t, errs[1], &throttled)
	require.NoError(t, errs[2])
}

func TestAWriteErrorFailsOnlyTheFirstUpsert(t *testing.T) {
	sink := openSink(t, connect(t, mock.WithWriteError("o1")), ordersPath)

	_, first := sink.Upsert(context.Background(), order("o1", "c01"))
	_, second := sink.Upsert(context.Background(), order("o1", "c01"))

	var injected *mock.InjectedError
	require.ErrorAs(t, first, &injected)
	require.NoError(t, second)
}

func TestTheGaugeCountsUpsertsInFlight(t *testing.T) {
	a := mock.New(mock.WithLatency(20 * time.Millisecond))
	sink := openSink(t, connectTo(t, a), ordersPath)

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sink.Upsert(context.Background(), order("o"+strconv.Itoa(i), "c01"))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, 4, a.Upserts())
	assert.GreaterOrEqual(t, a.HighestConcurrentUpserts(), 1)
	assert.LessOrEqual(t, a.HighestConcurrentUpserts(), 4)
}

func TestDeletingAContainerDropsItsItems(t *testing.T) {
	a := mock.New(mock.WithItemCount(ordersPath, 3))
	conn := connectTo(t, a)

	require.NoError(t, admin(t, conn).DeleteContainer(context.Background(), ordersPath))
	require.NoError(t, admin(t, conn).CreateContainer(context.Background(),
		adapter.ContainerSpec{Database: "sales", Name: "orders", PartitionKeys: []string{"/customerId"}}))

	assert.Empty(t, a.Items(ordersPath))
}
