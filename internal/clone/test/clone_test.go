package clone_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/clone"
)

var (
	ordersPath = []string{"sales", "orders"}
	copyPath   = []string{"sales", "orders-copy"}
)

// writerCounts are the pool sizes every engine behavior is checked at.
var writerCounts = []int{1, 8}

func eachWriterCount(t *testing.T, test func(t *testing.T, writers int)) {
	t.Helper()
	for _, n := range writerCounts {
		t.Run(fmt.Sprintf("%d writers", n), func(t *testing.T) { test(t, n) })
	}
}

// fakeClock answers every wait at once and keeps what was asked for.
type fakeClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

func (c *fakeClock) asked() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

func connect(t *testing.T, a *mock.Adapter) adapter.Connection {
	t.Helper()
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	return conn
}

func sourceOf(t *testing.T, a *mock.Adapter) clone.Source {
	t.Helper()
	conn := connect(t, a)
	definitions, _ := conn.(adapter.DefinitionReader)
	throughput, _ := conn.(adapter.ThroughputEditor)
	items, _ := conn.(adapter.ItemScanner)
	return clone.Source{Catalog: conn.Catalog(), Definitions: definitions, Throughput: throughput, Items: items}
}

func targetOf(t *testing.T, a *mock.Adapter) clone.Target {
	t.Helper()
	conn := connect(t, a)
	admin, _ := conn.(adapter.CatalogAdmin)
	items, _ := conn.(adapter.ItemWriter)
	return clone.Target{Catalog: conn.Catalog(), Admin: admin, Items: items}
}

func containerJob(writers int, target []string) clone.Job {
	return clone.Job{
		Source:  clone.Endpoint{Account: "source", Path: ordersPath},
		Target:  clone.Endpoint{Account: "target", Path: target},
		Content: clone.DefinitionAndItems,
		Writers: writers,
		Clock:   &fakeClock{},
	}
}

func prepare(t *testing.T, job clone.Job, from, to *mock.Adapter) clone.Plan {
	t.Helper()
	source, target := sourceOf(t, from), targetOf(t, to)
	survey, err := clone.SurveySource(context.Background(), source, job.Source)
	require.NoError(t, err)
	plan, err := clone.Prepare(context.Background(), job, survey, source, target)
	require.NoError(t, err)
	return plan
}

// runPlan runs every step of plan and returns the progress of each page.
func runPlan(t *testing.T, plan clone.Plan) []clone.Progress {
	t.Helper()
	ctx := context.Background()
	if plan.CreatesDatabase {
		require.NoError(t, plan.CreateDatabase(ctx))
	}
	var pages []clone.Progress
	for i := range plan.Containers {
		require.NoError(t, plan.CreateContainer(ctx, i))
		if plan.Job.Content == clone.DefinitionOnly {
			continue
		}
		pages = append(pages, copyAll(t, plan, i, "")...)
	}
	return pages
}

func copyAll(t *testing.T, plan clone.Plan, i int, from adapter.ScanPosition) []clone.Progress {
	t.Helper()
	c, err := plan.Open(context.Background(), i, from)
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	var pages []clone.Progress
	for !c.Done() {
		progress, err := c.CopyPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, progress)
	}
	return pages
}

func total(pages []clone.Progress) clone.Progress {
	var sum clone.Progress
	for _, p := range pages {
		sum.Read += p.Read
		sum.Written += p.Written
		sum.Skipped += p.Skipped
		sum.ReadCharge += p.ReadCharge
		sum.WriteCharge += p.WriteCharge
		sum.Throttles += p.Throttles
	}
	return sum
}

func ids(t *testing.T, items []json.RawMessage) []string {
	t.Helper()
	var listed []string
	for _, item := range items {
		var head struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(item, &head))
		listed = append(listed, head.ID)
	}
	sort.Strings(listed)
	return listed
}

func itemCalled(t *testing.T, items []json.RawMessage, id string) json.RawMessage {
	t.Helper()
	for _, item := range items {
		var head struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(item, &head))
		if head.ID == id {
			return item
		}
	}
	require.Fail(t, "no item "+id)
	return nil
}

func definitionOf(t *testing.T, a *mock.Adapter, path []string) adapter.ContainerDefinition {
	t.Helper()
	reader, ok := connect(t, a).(adapter.DefinitionReader)
	require.True(t, ok)
	def, err := reader.ContainerDefinition(context.Background(), path, adapter.DefinitionFull)
	require.NoError(t, err)
	return def
}

func TestADefinitionOnlyJobCreatesTheTargetAndWritesNoItem(t *testing.T) {
	a := mock.New(mock.WithItemCount(ordersPath, 12))
	job := containerJob(1, copyPath)
	job.Content = clone.DefinitionOnly

	runPlan(t, prepare(t, job, a, a))

	copied := definitionOf(t, a, copyPath)
	assert.Equal(t, []string{"/customerId"}, copied.PartitionKeys)
	assert.Equal(t, definitionOf(t, a, ordersPath).Policies, copied.Policies)
	assert.Empty(t, a.Items(copyPath))
	assert.Zero(t, a.Upserts())
}

func TestAPortableJobCarriesThePortableDefinition(t *testing.T) {
	a := mock.New()
	job := containerJob(1, copyPath)
	job.Content = clone.DefinitionOnly
	job.Fidelity = adapter.DefinitionPortable

	plan := prepare(t, job, a, a)

	assert.NotContains(t, string(plan.Containers[0].Spec.Policies.Raw), "vectorEmbeddingPolicy")
}

func TestAnExistingTargetIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	tests := []struct {
		name   string
		source []string
		target []string
	}{
		{name: "a container", source: ordersPath, target: []string{"sales", "customers"}},
		{name: "a database", source: []string{"sales"}, target: []string{"telemetry"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := mock.New()
			source, target := sourceOf(t, a), targetOf(t, a)
			job := containerJob(1, tt.target)
			job.Source.Path = tt.source
			survey, err := clone.SurveySource(context.Background(), source, job.Source)
			require.NoError(t, err)

			_, err = clone.Prepare(context.Background(), job, survey, source, target)

			require.ErrorIs(t, err, clone.ErrTargetExists)
		})
	}
}

func TestATargetThatAppearsAfterPrepareIsRefusedAtCreate(t *testing.T) {
	a := mock.New()
	plan := prepare(t, containerJob(1, copyPath), a, a)
	admin, ok := connect(t, a).(adapter.CatalogAdmin)
	require.True(t, ok)
	require.NoError(t, admin.CreateContainer(context.Background(), adapter.ContainerSpec{Database: "sales", Name: "orders-copy"}))

	err := plan.CreateContainer(context.Background(), 0)

	require.ErrorIs(t, err, clone.ErrTargetExists)
}

func TestAContainerCloneIntoANewDatabaseCreatesIt(t *testing.T) {
	a := mock.New()

	plan := prepare(t, containerJob(1, []string{"archive", "orders"}), a, a)

	assert.True(t, plan.CreatesDatabase)
	assert.Equal(t, adapter.DatabaseSpec{Name: "archive"}, plan.Database)
}

func TestCapacityChoices(t *testing.T) {
	manual400 := adapter.Throughput{Mode: adapter.ThroughputManual, RUs: clone.MinimumRUs}
	shared := adapter.Throughput{Mode: adapter.ThroughputShared}
	autoscale := adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 40000}
	tests := []struct {
		name     string
		source   []string
		target   []string
		sourceRU adapter.Throughput
		capacity clone.Capacity
		want     adapter.Throughput
	}{
		{name: "dedicated, minimum", source: ordersPath, target: copyPath, sourceRU: autoscale, capacity: clone.Minimum, want: manual400},
		{name: "dedicated, same as source", source: ordersPath, target: copyPath, sourceRU: autoscale, capacity: clone.SameAsSource, want: autoscale},
		{name: "dedicated, none", source: ordersPath, target: copyPath, sourceRU: autoscale, capacity: clone.None, want: adapter.Throughput{}},
		{name: "shared into an existing database stays shared", source: []string{"telemetry", "events"}, target: []string{"telemetry", "events-copy"}, capacity: clone.Minimum, want: shared},
		{name: "shared into a new database, minimum", source: []string{"telemetry", "events"}, target: []string{"archive", "events"}, capacity: clone.Minimum, want: manual400},
		{name: "shared into a new database, none", source: []string{"telemetry", "events"}, target: []string{"archive", "events"}, capacity: clone.None, want: adapter.Throughput{}},
		{name: "capacity-less, minimum", source: ordersPath, target: copyPath, sourceRU: adapter.Throughput{}, capacity: clone.Minimum, want: manual400},
		{name: "capacity-less, same as source", source: ordersPath, target: copyPath, sourceRU: adapter.Throughput{}, capacity: clone.SameAsSource, want: adapter.Throughput{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := mock.New()
			if tt.source[0] == "sales" {
				editor, ok := connect(t, a).(adapter.ThroughputEditor)
				require.True(t, ok)
				setThroughput(t, editor, tt.source, tt.sourceRU)
			}
			job := containerJob(1, tt.target)
			job.Source.Path = tt.source
			job.Capacity = tt.capacity

			plan := prepare(t, job, a, a)

			assert.Equal(t, tt.want, plan.Containers[0].Spec.Throughput)
		})
	}
}

// setThroughput provisions what the mock lets be set, and removes it by
// recreating the container when t provisions nothing.
func setThroughput(t *testing.T, editor adapter.ThroughputEditor, path []string, throughput adapter.Throughput) {
	t.Helper()
	if throughput.Provisioned() {
		require.NoError(t, editor.SetThroughput(context.Background(), path, throughput))
		return
	}
	admin, ok := editor.(adapter.CatalogAdmin)
	require.True(t, ok)
	require.NoError(t, admin.DeleteContainer(context.Background(), path))
	require.NoError(t, admin.CreateContainer(context.Background(),
		adapter.ContainerSpec{Database: path[0], Name: path[1], PartitionKeys: []string{"/customerId"}}))
}

func TestSameAsSourceNeedsTheSourcesThroughput(t *testing.T) {
	a := mock.New()
	source, target := sourceOf(t, a), targetOf(t, a)
	source.Throughput = nil
	job := containerJob(1, copyPath)
	job.Capacity = clone.SameAsSource
	survey, err := clone.SurveySource(context.Background(), source, job.Source)
	require.NoError(t, err)

	_, err = clone.Prepare(context.Background(), job, survey, source, target)

	require.Error(t, err)
}

func TestItemsWithoutAScannerOrAWriterAreRefused(t *testing.T) {
	a := mock.New()
	source, target := sourceOf(t, a), targetOf(t, a)
	target.Items = nil
	job := containerJob(1, copyPath)
	survey, err := clone.SurveySource(context.Background(), source, job.Source)
	require.NoError(t, err)

	_, err = clone.Prepare(context.Background(), job, survey, source, target)

	require.Error(t, err)
}

func TestEveryItemArrivesWithoutItsSystemFields(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		item := `{"id":"o1","customerId":"c01","ttl":3600,"big":12345678901234567890,` +
			`"_rid":"r","_self":"s","_attachments":"a/","_ts":1700000000}`
		a := mock.New(mock.WithItems(ordersPath, json.RawMessage(item)), mock.WithItemCount(ordersPath, 30))

		pages := runPlan(t, prepare(t, containerJob(writers, copyPath), a, a))

		assert.Equal(t, ids(t, a.Items(ordersPath)), ids(t, a.Items(copyPath)))
		sum := total(pages)
		assert.Equal(t, 31, sum.Read)
		assert.Equal(t, 31, sum.Written)
		assert.Positive(t, sum.ReadCharge)
		assert.Positive(t, sum.WriteCharge)
		for _, copied := range a.Items(copyPath) {
			for _, field := range []string{`"_rid"`, `"_self"`, `"_attachments"`, `"_ts":1700000000`} {
				assert.NotContains(t, string(copied), field)
			}
		}
		assert.Contains(t, string(itemCalled(t, a.Items(copyPath), "o1")),
			`{"id":"o1","customerId":"c01","ttl":3600,"big":12345678901234567890,"_etag":`,
			"the body is byte-identical but for the system fields the target gave it")
	})
}

func TestAnItemWithNoKeyValueIsSkippedAndCounted(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(
			mock.WithItemCount(ordersPath, 5),
			mock.WithItems(ordersPath, json.RawMessage(`{"id":"keyless"}`)),
		)

		pages := runPlan(t, prepare(t, containerJob(writers, copyPath), a, a))

		sum := total(pages)
		assert.Equal(t, 5, sum.Written)
		assert.Equal(t, 1, sum.Skipped)
		require.Len(t, pages[len(pages)-1].Skips, 1)
		assert.Equal(t, "keyless", pages[len(pages)-1].Skips[0].ID)
		assert.ErrorIs(t, pages[len(pages)-1].Skips[0].Reason, adapter.ErrNoPartitionKey)
	})
}

func TestTheSkipAfterTheLimitEndsTheCopy(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		var keyless []json.RawMessage
		for i := range clone.MaxSkipped + 1 {
			keyless = append(keyless, json.RawMessage(fmt.Sprintf(`{"id":"k%03d"}`, i)))
		}
		a := mock.New(mock.WithItems(ordersPath, keyless...))
		plan := prepare(t, containerJob(writers, copyPath), a, a)
		require.NoError(t, plan.CreateContainer(context.Background(), 0))
		c, err := plan.Open(context.Background(), 0, "")
		require.NoError(t, err)

		for err == nil && !c.Done() {
			_, err = c.CopyPage(context.Background())
		}

		require.ErrorIs(t, err, clone.ErrTooManySkipped)
	})
}

func TestWritesNeverExceedTheWriterCount(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 40), mock.WithLatency(time.Millisecond))

		runPlan(t, prepare(t, containerJob(writers, copyPath), a, a))

		assert.LessOrEqual(t, a.HighestConcurrentUpserts(), writers)
		assert.Len(t, a.Items(copyPath), 40)
	})
}

func TestAThrottledWriteIsRetriedAndTheWritersStepDown(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 25), mock.WithThrottle("item-00003", 1, 2*time.Second))
		job := containerJob(writers, copyPath)
		clock := &fakeClock{}
		job.Clock = clock

		pages := runPlan(t, prepare(t, job, a, a))

		sum := total(pages)
		assert.Equal(t, 25, sum.Written)
		assert.Equal(t, 1, sum.Throttles)
		assert.Len(t, a.Items(copyPath), 25)
		assert.Equal(t, []time.Duration{2 * time.Second}, clock.asked())
		assert.Equal(t, max(writers-1, 1), pages[len(pages)-1].Writers)
	})
}

func TestTenThrottlesInARowEndTheStepAndKeepThePosition(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 25), mock.WithThrottle("item-00013", 10, 0))
		plan := prepare(t, containerJob(writers, copyPath), a, a)
		require.NoError(t, plan.CreateContainer(context.Background(), 0))
		c, err := plan.Open(context.Background(), 0, "")
		require.NoError(t, err)

		_, first := c.CopyPage(context.Background())
		_, second := c.CopyPage(context.Background())

		require.NoError(t, first)
		var throttled *adapter.ThrottledError
		require.ErrorAs(t, second, &throttled)
		assert.Equal(t, adapter.ScanPosition("10"), c.Position(), "the last complete page")
	})
}

func TestAResumedCopyWritesEveryItemOnce(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 25), mock.WithWriteError("item-00015"))
		plan := prepare(t, containerJob(writers, copyPath), a, a)
		require.NoError(t, plan.CreateContainer(context.Background(), 0))
		c, err := plan.Open(context.Background(), 0, "")
		require.NoError(t, err)
		for err == nil {
			_, err = c.CopyPage(context.Background())
		}
		var injected *mock.InjectedError
		require.ErrorAs(t, err, &injected)

		copyAll(t, plan, 0, c.Position())

		assert.Equal(t, ids(t, a.Items(ordersPath)), ids(t, a.Items(copyPath)))
	})
}

func TestACancelledContextStartsNoWrite(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 25))
		plan := prepare(t, containerJob(writers, copyPath), a, a)
		require.NoError(t, plan.CreateContainer(context.Background(), 0))
		c, err := plan.Open(context.Background(), 0, "")
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = c.CopyPage(ctx)

		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, a.Upserts())
		assert.Empty(t, c.Position())
	})
}

func TestADatabaseJobCopiesItsContainersInCatalogOrder(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(
			mock.WithItemCount([]string{"telemetry", "events"}, 12),
			mock.WithItemCount([]string{"telemetry", "alerts"}, 3),
		)
		job := containerJob(writers, []string{"telemetry-copy"})
		job.Source.Path = []string{"telemetry"}

		plan := prepare(t, job, a, a)
		runPlan(t, plan)

		var order []string
		for _, container := range plan.Containers {
			order = append(order, container.Spec.Name)
		}
		assert.Equal(t, []string{"events", "devices", "alerts"}, order)
		assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputManual, RUs: clone.MinimumRUs}, plan.Database.Throughput)
		assert.Equal(t, adapter.ThroughputShared, plan.Containers[0].Spec.Throughput.Mode)
		assert.Equal(t, adapter.ThroughputManual, plan.Containers[1].Spec.Throughput.Mode)
		assert.Len(t, a.Items([]string{"telemetry-copy", "events"}), 12)
		assert.Len(t, a.Items([]string{"telemetry-copy", "alerts"}), 3)
	})
}

func TestAFailureInTheSecondContainerLeavesTheFirstComplete(t *testing.T) {
	a := mock.New(
		mock.WithItemCount([]string{"telemetry", "events"}, 12),
		mock.WithItemCount([]string{"telemetry", "devices"}, 12),
		mock.WithItems([]string{"telemetry", "devices"}, json.RawMessage(`{"id":"unwritable","deviceId":"d1"}`)),
		mock.WithWriteError("unwritable"),
	)
	job := containerJob(1, []string{"telemetry-copy"})
	job.Source.Path = []string{"telemetry"}
	plan := prepare(t, job, a, a)
	ctx := context.Background()
	require.NoError(t, plan.CreateDatabase(ctx))
	require.NoError(t, plan.CreateContainer(ctx, 0))
	copyAll(t, plan, 0, "")
	require.NoError(t, plan.CreateContainer(ctx, 1))
	c, err := plan.Open(ctx, 1, "")
	require.NoError(t, err)

	for err == nil && !c.Done() {
		_, err = c.CopyPage(ctx)
	}

	assert.Len(t, a.Items([]string{"telemetry-copy", "events"}), 12)
	var injected *mock.InjectedError
	require.ErrorAs(t, err, &injected)
	assert.Equal(t, adapter.ScanPosition("10"), c.Position())
}

func TestStripSystemFieldsKeepsEverythingElse(t *testing.T) {
	got, err := clone.StripSystemFields(json.RawMessage(`{"id":"a","_etag":"x","n":1.50,"_ts":1}`))

	require.NoError(t, err)
	assert.Equal(t, `{"id":"a","n":1.50}`, string(got))
}

func TestASurveyAddsUpItsContainers(t *testing.T) {
	a := mock.New(mock.WithItemCount([]string{"telemetry", "events"}, 4), mock.WithItemCount([]string{"telemetry", "alerts"}, 2))

	survey, err := clone.SurveySource(context.Background(), sourceOf(t, a), clone.Endpoint{Path: []string{"telemetry"}})

	require.NoError(t, err)
	assert.Equal(t, int64(6), survey.Size().Items)
	assert.True(t, survey.Size().Known)
	assert.True(t, survey.ThroughputKnown)
	assert.Equal(t, adapter.ThroughputManual, survey.Throughput.Mode)
}

func TestASurveyOfAnUnknownSizeSaysSo(t *testing.T) {
	survey, err := clone.SurveySource(context.Background(), sourceOf(t, mock.New(mock.WithUnknownSize())),
		clone.Endpoint{Path: ordersPath})

	require.NoError(t, err)
	assert.False(t, survey.Size().Known)
}

func TestErrorsNameTheTarget(t *testing.T) {
	a := mock.New(mock.WithError(mock.OpCreateContainer))
	plan := prepare(t, containerJob(1, copyPath), a, a)

	err := plan.CreateContainer(context.Background(), 0)

	require.Error(t, err)
	assert.False(t, errors.Is(err, clone.ErrTargetExists))
	assert.Contains(t, err.Error(), "target/sales.orders-copy")
}

// cancellingWriter cancels the step on its first upsert, and counts the
// upserts that found their own context cancelled.
type cancellingWriter struct {
	adapter.ItemWriter
	cancel    context.CancelFunc
	once      sync.Once
	mu        sync.Mutex
	cancelled int
}

func (w *cancellingWriter) OpenItemSink(ctx context.Context, path []string) (adapter.ItemSink, error) {
	sink, err := w.ItemWriter.OpenItemSink(ctx, path)
	return cancellingSink{ItemSink: sink, writer: w}, err
}

type cancellingSink struct {
	adapter.ItemSink
	writer *cancellingWriter
}

func (s cancellingSink) Upsert(ctx context.Context, item json.RawMessage) (float64, error) {
	s.writer.once.Do(s.writer.cancel)
	if ctx.Err() != nil {
		s.writer.mu.Lock()
		s.writer.cancelled++
		s.writer.mu.Unlock()
	}
	return s.ItemSink.Upsert(ctx, item)
}

func TestAStoppedPageLetsTheWritesInFlightFinish(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, writers int) {
		a := mock.New(mock.WithItemCount(ordersPath, 25))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source, target := sourceOf(t, a), targetOf(t, a)
		writer := &cancellingWriter{ItemWriter: target.Items, cancel: cancel}
		target.Items = writer
		job := containerJob(writers, copyPath)
		survey, err := clone.SurveySource(context.Background(), source, job.Source)
		require.NoError(t, err)
		plan, err := clone.Prepare(context.Background(), job, survey, source, target)
		require.NoError(t, err)
		require.NoError(t, plan.CreateContainer(context.Background(), 0))
		c, err := plan.Open(context.Background(), 0, "")
		require.NoError(t, err)

		_, err = c.CopyPage(ctx)

		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, writer.cancelled, "no write in flight saw the stop")
		assert.Equal(t, a.Upserts(), len(a.Items(copyPath)), "every write that started was written")
		assert.Empty(t, c.Position(), "the page is written again on resume")
	})
}
