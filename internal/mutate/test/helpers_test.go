package mutate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/writers"
)

var (
	ordersPath   = []string{"sales", "orders"}
	keyPaths     = []string{"/customerId"}
	writerCounts = []int{1, 8}
)

const (
	shipped  = `o.status = "shipped"`
	archive  = `UPDATE sales.orders o SET o.status = "archived" WHERE ` + shipped
	cheap    = `o.total < 50`
	flagAll  = `UPDATE sales.orders o SET o.flag = true WHERE ` + cheap
	dropNote = `UPDATE sales.orders o UNSET o.note WHERE ` + shipped
)

func eachWriterCount(t *testing.T, test func(t *testing.T, writers int)) {
	t.Helper()
	for _, n := range writerCounts {
		t.Run(fmt.Sprintf("%d writers", n), func(t *testing.T) { test(t, n) })
	}
}

func order(i int, status string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":"o%03d","customerId":"c%02d","status":%q,"total":%d}`, i, i%7, status, i))
}

func orders(n int, status string) []json.RawMessage {
	items := make([]json.RawMessage, n)
	for i := range items {
		items[i] = order(i, status)
	}
	return items
}

func field(name string, want func(json.RawMessage) bool) func(json.RawMessage) bool {
	return func(item json.RawMessage) bool {
		var fields map[string]json.RawMessage
		return json.Unmarshal(item, &fields) == nil && want(fields[name])
	}
}

func equals(value string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool { return string(raw) == value }
}

func below(limit float64) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var n float64
		return json.Unmarshal(raw, &n) == nil && n < limit
	}
}

// store is a mock holding items, which knows what the tests' predicates
// mean.
func store(items []json.RawMessage, opts ...mock.Option) *mock.Adapter {
	return mock.New(append([]mock.Option{
		mock.WithPredicate(shipped, field("status", equals(`"shipped"`))),
		mock.WithPredicate(cheap, field("total", below(50))),
		mock.WithPredicate("true", func(json.RawMessage) bool { return true }),
		mock.WithItems(ordersPath, items...),
	}, opts...)...)
}

func connection(t *testing.T, a *mock.Adapter) adapter.Connection {
	t.Helper()
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	return conn
}

func scannerOf(t *testing.T, a *mock.Adapter) adapter.ItemScanner {
	t.Helper()
	scanner, ok := connection(t, a).(adapter.ItemScanner)
	require.True(t, ok)
	return scanner
}

func editorOf(t *testing.T, a *mock.Adapter) adapter.ItemEditor {
	t.Helper()
	editor, ok := connection(t, a).(adapter.ItemEditor)
	require.True(t, ok)
	return editor
}

func parse(t *testing.T, statement string) query.Mutation {
	t.Helper()
	m, err := query.ParseMutation(statement)
	require.NoError(t, err)
	return m
}

// selectTargets runs a selection to its end.
func selectTargets(t *testing.T, a *mock.Adapter, m query.Mutation, limit int) (mutate.Targets, error) {
	t.Helper()
	selection, err := mutate.Select(context.Background(), scannerOf(t, a), m, keyPaths, limit)
	require.NoError(t, err)
	defer func() { require.NoError(t, selection.Close()) }()
	for !selection.Done() {
		if _, err := selection.Next(context.Background()); err != nil {
			return selection.Targets(), err
		}
	}
	return selection.Targets(), nil
}

func mustSelect(t *testing.T, a *mock.Adapter, statement string) (query.Mutation, mutate.Targets) {
	t.Helper()
	m := parse(t, statement)
	targets, err := selectTargets(t, a, m, 0)
	require.NoError(t, err)
	return m, targets
}

func newJob(m query.Mutation, targets mutate.Targets, editor adapter.ItemEditor, writerCount int, clock writers.Clock) *mutate.Job {
	return mutate.NewJob(m, targets, editor, writers.NewPool(writerCount, clock))
}

// runJob applies chunks until the job is done or a step ends it.
func runJob(j *mutate.Job) (mutate.Progress, error) {
	var progress mutate.Progress
	for !j.Done() {
		var err error
		if progress, err = j.ApplyChunk(context.Background()); err != nil {
			return progress, err
		}
	}
	return progress, nil
}

// update selects and writes statement over a with n writers.
func update(t *testing.T, a *mock.Adapter, statement string, n int) (*mutate.Job, error) {
	t.Helper()
	m, targets := mustSelect(t, a, statement)
	j := newJob(m, targets, editorOf(t, a), n, &fakeClock{})
	_, err := runJob(j)
	return j, err
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

// recordingEditor keeps every operation it passes on.
type recordingEditor struct {
	adapter.ItemEditor

	mu  sync.Mutex
	ops []adapter.Operation
}

func (e *recordingEditor) EditItem(ctx context.Context, container []string, key adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	e.mu.Lock()
	e.ops = append(e.ops, op)
	e.mu.Unlock()
	return e.ItemEditor.EditItem(ctx, container, key, op)
}

func (e *recordingEditor) sent() []adapter.Operation {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]adapter.Operation(nil), e.ops...)
}

func storedField(t *testing.T, a *mock.Adapter, id, name string) string {
	t.Helper()
	for _, item := range a.Items(ordersPath) {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(item, &fields))
		if string(fields["id"]) == `"`+id+`"` {
			return string(fields[name])
		}
	}
	return ""
}

func ids(n int) []string {
	listed := make([]string, n)
	for i := range listed {
		listed[i] = fmt.Sprintf("o%03d", i)
	}
	return listed
}
