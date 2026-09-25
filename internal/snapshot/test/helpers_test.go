package snapshot_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/canonical"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

var ordersPath = []string{"sales", "orders"}

// clock is a test's time: it starts on a fixed day and moves only when
// told to, so snapshot ids and modified times are the test's to choose.
type clock struct {
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time { return c.now }

func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fixture is a mock account, a clock both sides read, and a store for its
// sales.orders.
type fixture struct {
	t     testing.TB
	clock *clock
	mock  *mock.Adapter
	conn  adapter.Connection
	loc   snapshot.Location
}

func newFixture(t *testing.T, items ...json.RawMessage) *fixture {
	t.Helper()
	c := newClock()
	a := mock.New(mock.WithClock(c.Now), mock.WithItems(ordersPath, items...))
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	return &fixture{
		t:     t,
		clock: c,
		mock:  a,
		conn:  conn,
		loc:   snapshot.Location{Root: t.TempDir(), Account: "prod", Database: "sales", Container: "orders"},
	}
}

func (f *fixture) source() snapshot.Source {
	return snapshot.Source{
		Container:   ordersPath,
		Items:       f.conn.(adapter.ItemScanner),
		Definitions: f.conn.(adapter.DefinitionReader),
		Throughput:  f.conn.(adapter.ThroughputEditor),
	}
}

func (f *fixture) open() *snapshot.Store {
	f.t.Helper()
	store, err := snapshot.Open(f.loc)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _ = store.Close() })
	return store
}

// take captures a snapshot to the end, a page at a time, a minute after
// whatever happened last.
func (f *fixture) take(options snapshot.CaptureOptions) snapshot.Record {
	f.t.Helper()
	f.clock.advance(time.Minute)
	return takeFrom(f.t, f.open(), f.source(), f.withClock(options))
}

func (f *fixture) withClock(options snapshot.CaptureOptions) snapshot.CaptureOptions {
	options.Clock = f.clock.Now
	options.PageSize = 7
	return options
}

func takeFrom(t testing.TB, store *snapshot.Store, source snapshot.Source, options snapshot.CaptureOptions) snapshot.Record {
	t.Helper()
	capture, err := store.Begin(source, options)
	require.NoError(t, err)
	for {
		progress, err := capture.Next(context.Background())
		require.NoError(t, err)
		if progress.Done {
			return progress.Record
		}
	}
}

// put writes items into sales.orders a second after whatever happened last.
func (f *fixture) put(items ...string) {
	f.t.Helper()
	f.clock.advance(time.Second)
	for _, item := range items {
		require.NoError(f.t, f.mock.PutItem(ordersPath, json.RawMessage(item)))
	}
}

func (f *fixture) remove(customer, id string) {
	f.t.Helper()
	f.clock.advance(time.Second)
	require.NoError(f.t, f.mock.DeleteItem(ordersPath, id, keyOf(customer)))
}

func keyOf(customer string) adapter.PartitionKey {
	return adapter.PartitionKey{json.RawMessage(fmt.Sprintf("%q", customer))}
}

// held is what the mock holds now, each item as its canonical body without
// system fields, sorted: what an export of a snapshot taken now must equal.
func (f *fixture) held() []string {
	f.t.Helper()
	var bodies []string
	for _, item := range f.mock.Items(ordersPath) {
		body, _, err := adapter.SplitSystemFields(item)
		require.NoError(f.t, err)
		encoded, err := canonical.Marshal(body)
		require.NoError(f.t, err)
		bodies = append(bodies, string(encoded))
	}
	slices.Sort(bodies)
	return bodies
}

// exported writes snapshot id's items as .jsonl and returns them sorted.
func exported(t *testing.T, store *snapshot.Store, id string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "items.jsonl")
	require.NoError(t, store.WriteItems(path, id, snapshot.RefuseExisting))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	slices.Sort(lines)
	return lines
}

func order(id, customer string, total float64) string {
	return fmt.Sprintf(`{"id":%q,"customerId":%q,"status":"open","total":%v}`, id, customer, total)
}

func orders(n int) []json.RawMessage {
	items := make([]json.RawMessage, n)
	for i := range items {
		items[i] = json.RawMessage(order(fmt.Sprintf("o-%04d", i), fmt.Sprintf("c%02d", i%7), float64(i)))
	}
	return items
}

func isRefused(err error) bool {
	return errors.Is(err, snapshot.ErrCorrupt) || errors.Is(err, snapshot.ErrUnknownFormat)
}
