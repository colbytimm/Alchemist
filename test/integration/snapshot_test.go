//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

const (
	snapshotDatabase = "alchemist_snapshot_it"
	snapshotOrders   = 200
	// padding makes an order about 2 KB, so reading bodies costs more than
	// reading keys, as it does in a real container.
	padding = 1800
)

func snapshotConnection(t *testing.T) adapter.Connection {
	t.Helper()
	raw := settings()
	raw["page_size"] = "50"
	conn, err := cosmos.Adapter{}.Connect(context.Background(), raw)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	waitForEmulator(t, conn)
	return conn
}

func snapshotOrder(i int, status string) map[string]any {
	return map[string]any{
		"id": fmt.Sprintf("o%03d", i), "customerId": fmt.Sprintf("c%02d", i%12), "status": status,
		"total": i * 10, "notes": strings.Repeat("x", padding),
	}
}

// seedSnapshotDatabase makes orders, keyed on /customerId, with one order
// that has no customerId at all, and customers.
func seedSnapshotDatabase(t *testing.T) *azcosmos.ContainerClient {
	t.Helper()
	client := seedClient(t)
	db := freshDatabase(t, client, snapshotDatabase)
	ctx := context.Background()
	for _, name := range []string{"orders", "customers"} {
		_, err := db.CreateContainer(ctx, azcosmos.ContainerProperties{ID: name, PartitionKeyDefinition: singleKey("/customerId")}, nil)
		require.NoError(t, err)
	}
	orders, err := db.NewContainer("orders")
	require.NoError(t, err)
	for i := range snapshotOrders {
		upsertOrder(t, orders, snapshotOrder(i, "open"))
	}
	body, err := json.Marshal(map[string]any{"id": "keyless", "status": "open"})
	require.NoError(t, err)
	_, err = orders.CreateItem(ctx, azcosmos.NewPartitionKey(), body, nil)
	require.NoError(t, err)
	customers, err := db.NewContainer("customers")
	require.NoError(t, err)
	body, err = json.Marshal(map[string]any{"id": "c01", "customerId": "c01"})
	require.NoError(t, err)
	_, err = customers.CreateItem(ctx, azcosmos.NewPartitionKeyString("c01"), body, nil)
	require.NoError(t, err)
	return orders
}

func upsertOrder(t *testing.T, orders *azcosmos.ContainerClient, order map[string]any) {
	t.Helper()
	body, err := json.Marshal(order)
	require.NoError(t, err)
	customer, ok := order["customerId"].(string)
	require.True(t, ok)
	_, err = orders.UpsertItem(context.Background(), azcosmos.NewPartitionKeyString(customer), body, nil)
	require.NoError(t, err)
}

func snapshotSource(conn adapter.Connection, container string) snapshot.Source {
	definitions, _ := conn.(adapter.DefinitionReader)
	throughput, _ := conn.(adapter.ThroughputEditor)
	items, _ := conn.(adapter.ItemScanner)
	return snapshot.Source{Container: []string{snapshotDatabase, container}, Items: items, Definitions: definitions, Throughput: throughput}
}

func captureOnce(t *testing.T, loc snapshot.Location, source snapshot.Source) snapshot.Record {
	t.Helper()
	store, err := snapshot.Open(loc)
	require.NoError(t, err)
	capture, err := store.Begin(source, snapshot.CaptureOptions{})
	require.NoError(t, err)
	for {
		progress, err := capture.Next(context.Background())
		require.NoError(t, err)
		if progress.Done {
			return progress.Record
		}
	}
}

// pastTheSecond waits until the service's one-second clock has moved on,
// so what happens next has a later _ts than anything before.
func pastTheSecond() time.Time {
	time.Sleep(1100 * time.Millisecond)
	return time.Now()
}

func TestIntegrationSnapshotsAndDiff(t *testing.T) {
	conn := snapshotConnection(t)
	orders := seedSnapshotDatabase(t)
	loc := snapshot.Location{Root: t.TempDir(), Account: "emulator", Database: snapshotDatabase, Container: "orders"}
	pastTheSecond()

	first := captureOnce(t, loc, snapshotSource(conn, "orders"))
	since := pastTheSecond()
	upsertOrder(t, orders, snapshotOrder(3, "shipped"))
	shipped := snapshotOrder(4, "shipped")
	shipped["total"] = 999
	upsertOrder(t, orders, shipped)
	upsertOrder(t, orders, snapshotOrder(snapshotOrders, "open"))
	_, err := orders.DeleteItem(context.Background(), azcosmos.NewPartitionKeyString("c05"), "o005", nil)
	require.NoError(t, err)
	second := captureOnce(t, loc, snapshotSource(conn, "orders"))

	assert.Equal(t, snapshot.ModeFull, first.Mode)
	assert.Equal(t, int64(snapshotOrders+1), first.Items)
	assert.Equal(t, snapshot.ModeIncremental, second.Mode)
	assert.Positive(t, second.RequestCharge)
	// The vnext-preview emulator charges a flat 1 RU a page whatever it
	// returns, so bytes read are what show a capture reading keys and the
	// changed bodies alone.
	assert.Less(t, second.ReadBytes, first.ReadBytes/4)
	t.Logf("RU: full capture %.2f, incremental capture %.2f; read %d and %d bytes",
		first.RequestCharge, second.RequestCharge, first.ReadBytes, second.ReadBytes)
	store, err := snapshot.Open(loc)
	require.NoError(t, err)
	d, err := store.Diff(first.ID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, [3]int{1, 1, 2}, [3]int{d.Count(snapshot.Added), d.Count(snapshot.Removed), d.Count(snapshot.Modified)})
	changed := map[string][]string{}
	for _, item := range d.Items {
		fields, err := store.ChangedFields(item)
		require.NoError(t, err)
		changed[item.Kind.String()+" "+item.Identity.ID] = fields
	}
	assert.Equal(t, map[string][]string{
		"added o200": nil, "removed o005": nil, "modified o003": {"status"}, "modified o004": {"status", "total"},
	}, changed)
	assert.Equal(t, snapshot.DefinitionCompared, d.Definition.State)
	assert.Empty(t, d.Definition.Changes)

	assertSinceScan(t, conn, since, []string{"o003", "o004", "o200"})
	logSweepCharges(t, orders)
}

func assertSinceScan(t *testing.T, conn adapter.Connection, since time.Time, want []string) {
	t.Helper()
	items := scanEvery(t, conn, adapter.ScanRequest{Container: []string{snapshotDatabase, "orders"}, Since: since})
	var ids []string
	for _, item := range items {
		body, _, err := adapter.SplitSystemFields(item)
		require.NoError(t, err)
		var head struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(body, &head))
		ids = append(ids, head.ID)
	}
	slices.Sort(ids)
	assert.Equal(t, want, ids)
}

func scanEvery(t *testing.T, conn adapter.Connection, request adapter.ScanRequest) []json.RawMessage {
	t.Helper()
	scanner, ok := conn.(adapter.ItemScanner)
	require.True(t, ok)
	scan, err := scanner.ScanItems(context.Background(), request)
	require.NoError(t, err)
	var items []json.RawMessage
	for scan.HasMore() {
		page, err := scan.NextPage(context.Background())
		require.NoError(t, err)
		items = append(items, page.Items...)
	}
	return items
}

// logSweepCharges measures what a full scan, and the sweep with and without
// _etag, cost on the emulator: the figures behind the sweep's projection.
func logSweepCharges(t *testing.T, orders *azcosmos.ContainerClient) {
	t.Helper()
	for _, text := range []string{
		"SELECT * FROM c",
		"SELECT VALUE " + cosmos.IdentityProjection([]string{"/customerId"}) + " FROM c",
		`SELECT VALUE {"id": c.id, "_ts": c._ts, "customerId": c.customerId} FROM c`,
	} {
		pager := orders.NewQueryItemsPager(text, azcosmos.NewPartitionKey(), nil)
		var charge float32
		for pager.More() {
			page, err := pager.NextPage(context.Background())
			require.NoError(t, err)
			charge += page.RequestCharge
		}
		t.Logf("RU %.2f for %s", charge, text)
	}
}

func TestIntegrationIdentityScan(t *testing.T) {
	conn := snapshotConnection(t)
	seedSnapshotDatabase(t)

	items := scanEvery(t, conn, adapter.ScanRequest{Container: []string{snapshotDatabase, "orders"}, Projection: adapter.ScanIdentity})

	require.Len(t, items, snapshotOrders+1)
	for _, item := range items {
		body, meta, err := adapter.SplitSystemFields(item)
		require.NoError(t, err)
		assert.NotEmpty(t, meta.Version)
		assert.False(t, meta.Modified.IsZero())
		var fields map[string]any
		require.NoError(t, json.Unmarshal(body, &fields))
		if fields["id"] == "keyless" {
			assert.Equal(t, map[string]any{"id": "keyless"}, fields, "no key value for an item without one")
			continue
		}
		assert.ElementsMatch(t, []string{"id", "customerId"}, keys(fields))
	}
}

func keys(m map[string]any) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}

func TestIntegrationDatabaseSnapshot(t *testing.T) {
	conn := snapshotConnection(t)
	seedSnapshotDatabase(t)
	loc := snapshot.Location{Root: t.TempDir(), Account: "emulator", Database: snapshotDatabase}
	throughput, _ := conn.(adapter.ThroughputEditor)
	source := snapshot.GroupSource{Throughput: throughput}
	for _, name := range []string{"customers", "orders"} {
		source.Containers = append(source.Containers, snapshotSource(conn, name))
	}

	capture := snapshot.BeginGroup(loc, source, snapshot.CaptureOptions{})
	var group snapshot.Group
	for {
		progress, err := capture.Next(context.Background())
		require.NoError(t, err)
		if progress.Done {
			group = progress.Group
			break
		}
	}

	require.Len(t, group.Containers, 2)
	for _, member := range group.Containers {
		assert.NotEmpty(t, member.Snapshot, "%s: %s", member.Container, member.Error)
	}
	groups, err := snapshot.Groups(loc)
	require.NoError(t, err)
	assert.Len(t, groups, 1)
}
