//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/writers"
)

// deleteFixture is a connection to the emulator and a fresh database of
// its own, which the test fills with every item it reads.
type deleteFixture struct {
	database string
	db       *azcosmos.DatabaseClient
	scanner  adapter.ItemScanner
	editor   adapter.ItemEditor
}

func newDeleteFixture(t *testing.T, database string) deleteFixture {
	t.Helper()
	conn := connectWithRetry(t)
	f := deleteFixture{database: database, db: freshDatabase(t, seedClient(t), database)}
	var ok bool
	f.scanner, ok = conn.(adapter.ItemScanner)
	require.True(t, ok)
	f.editor, ok = conn.(adapter.ItemEditor)
	require.True(t, ok)
	return f
}

func (f deleteFixture) create(t *testing.T, name string, key azcosmos.PartitionKeyDefinition, items ...map[string]any) *azcosmos.ContainerClient {
	t.Helper()
	ctx := context.Background()
	_, err := f.db.CreateContainer(ctx, azcosmos.ContainerProperties{ID: name, PartitionKeyDefinition: key}, nil)
	require.NoError(t, err)
	container, err := f.db.NewContainer(name)
	require.NoError(t, err)
	for _, item := range items {
		body, err := json.Marshal(item)
		require.NoError(t, err)
		_, err = container.CreateItem(ctx, keyOf(t, body, key.Paths), body, nil)
		require.NoError(t, err)
	}
	return container
}

func keyOf(t *testing.T, body json.RawMessage, paths []string) azcosmos.PartitionKey {
	t.Helper()
	values, err := adapter.PartitionKeyValues(body, paths)
	require.NoError(t, err)
	pk := azcosmos.NewPartitionKey()
	for _, value := range values {
		var s string
		require.NoError(t, json.Unmarshal(value, &s))
		pk = pk.AppendString(s)
	}
	return pk
}

func (f deleteFixture) selectTargets(t *testing.T, statement string, keyPaths []string) (query.Mutation, mutate.Targets) {
	t.Helper()
	m, err := query.ParseMutation(statement)
	require.NoError(t, err)
	selection, err := mutate.Select(context.Background(), f.scanner, m, keyPaths, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, selection.Close()) }()
	for !selection.Done() {
		_, err := selection.Next(context.Background())
		require.NoError(t, err)
	}
	return m, selection.Targets()
}

func (f deleteFixture) apply(t *testing.T, m query.Mutation, targets mutate.Targets, writerCount int) mutate.Summary {
	t.Helper()
	j := mutate.NewJob(m, targets, f.editor, writers.NewPool(writerCount, writers.SystemClock{}))
	for !j.Done() {
		_, err := j.ApplyChunk(context.Background())
		require.NoError(t, err)
	}
	summary := j.Summary()
	assert.GreaterOrEqual(t, summary.WriteCharge, 0.0)
	return summary
}

func (f deleteFixture) run(t *testing.T, statement string, keyPaths []string, writerCount int) mutate.Summary {
	t.Helper()
	m, targets := f.selectTargets(t, statement, keyPaths)
	return f.apply(t, m, targets, writerCount)
}

func countOf(t *testing.T, container *azcosmos.ContainerClient, text string) int {
	t.Helper()
	total := 0
	pager := container.NewQueryItemsPager(text, azcosmos.NewPartitionKey(), nil)
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		require.NoError(t, err)
		for _, raw := range page.Items {
			var n int
			require.NoError(t, json.Unmarshal(raw, &n))
			total += n
		}
	}
	return total
}

// salesOrders is 30 orders across six customers: 12 cancelled, 10 open, 8
// shipped.
func salesOrders() []map[string]any {
	statuses := []string{"cancelled", "open", "shipped", "cancelled", "open"}
	var orders []map[string]any
	for i := range 30 {
		status := statuses[i%len(statuses)]
		if i >= 25 {
			status = []string{"cancelled", "cancelled", "shipped", "shipped", "shipped"}[i-25]
		}
		orders = append(orders, map[string]any{
			"id": fmt.Sprintf("o%03d", i), "customerId": fmt.Sprintf("c%02d", i%6), "status": status, "total": i * 3,
		})
	}
	return orders
}

func TestIntegrationDeleteByQuery(t *testing.T) {
	f := newDeleteFixture(t, "d22_delete_goal")
	orders := f.create(t, "orders", singleKey("/customerId"), salesOrders()...)
	statusCount := func(status string) int {
		return countOf(t, orders, fmt.Sprintf(`SELECT VALUE COUNT(1) FROM c WHERE c.status = %q`, status))
	}
	require.Equal(t, 12, statusCount("cancelled"))
	goal := `DELETE FROM d22_delete_goal.orders o WHERE o.status = "cancelled"`

	summary := f.run(t, goal, []string{"/customerId"}, 4)

	assert.Equal(t, mutate.Counts{Applied: 12}, summary.Counts)
	assert.Zero(t, statusCount("cancelled"))
	assert.Equal(t, 10, statusCount("open"))
	assert.Equal(t, 8, statusCount("shipped"))

	t.Run("a second run selects none", func(t *testing.T) {
		_, targets := f.selectTargets(t, goal, []string{"/customerId"})

		assert.Empty(t, targets.Items)
	})

	t.Run("an item replaced since the selection survives with its new body", func(t *testing.T) {
		statement := `DELETE FROM d22_delete_goal.orders o WHERE o.status = "open"`
		m, targets := f.selectTargets(t, statement, []string{"/customerId"})
		require.Len(t, targets.Items, 10)
		first := targets.Items[0]
		key := keyOf(t, json.RawMessage(fmt.Sprintf(`{"customerId":%s}`, first.Key[0])), []string{"/customerId"})
		replaced := fmt.Sprintf(`{"id":%q,"customerId":%s,"status":"open","note":"edited elsewhere"}`, first.ID, first.Key[0])
		_, err := orders.ReplaceItem(context.Background(), key, first.ID, []byte(replaced), nil)
		require.NoError(t, err)

		summary := f.apply(t, m, targets, 1)

		assert.Equal(t, mutate.Counts{Applied: 9, Changed: 1}, summary.Counts)
		resp, err := orders.ReadItem(context.Background(), key, first.ID, nil)
		require.NoError(t, err)
		var kept map[string]any
		require.NoError(t, json.Unmarshal(resp.Value, &kept))
		assert.Equal(t, "edited elsewhere", kept["note"])
	})

	t.Run("an item deleted since the selection is gone", func(t *testing.T) {
		m, targets := f.selectTargets(t, `DELETE FROM d22_delete_goal.orders o WHERE o.status = "shipped"`, []string{"/customerId"})
		require.Len(t, targets.Items, 8)
		first := targets.Items[0]
		key := keyOf(t, json.RawMessage(fmt.Sprintf(`{"customerId":%s}`, first.Key[0])), []string{"/customerId"})
		_, err := orders.DeleteItem(context.Background(), key, first.ID, nil)
		require.NoError(t, err)

		summary := f.apply(t, m, targets, 2)

		assert.Equal(t, mutate.Counts{Applied: 7, Gone: 1}, summary.Counts)
		assert.True(t, summary.Clean())
	})
}

func TestIntegrationDeleteEveryItem(t *testing.T) {
	f := newDeleteFixture(t, "d22_delete_every")
	var archived []map[string]any
	for i := range 15 {
		archived = append(archived, map[string]any{"id": fmt.Sprintf("a%02d", i), "customerId": fmt.Sprintf("c%02d", i%4)})
	}
	archive := f.create(t, "archive", singleKey("/customerId"), archived...)

	summary := f.run(t, `DELETE FROM d22_delete_every.archive a WHERE true`, []string{"/customerId"}, 4)

	assert.Equal(t, mutate.Counts{Applied: 15}, summary.Counts)
	assert.Zero(t, countOf(t, archive, "SELECT VALUE COUNT(1) FROM c"))
}

func TestIntegrationDeleteThroughThePool(t *testing.T) {
	f := newDeleteFixture(t, "d22_delete_pool")
	var events []map[string]any
	for i := range 300 {
		events = append(events, map[string]any{"id": fmt.Sprintf("e%03d", i), "deviceId": fmt.Sprintf("d%02d", i%20), "level": []string{"info", "debug"}[i%2]})
	}
	container := f.create(t, "events", singleKey("/deviceId"), events...)

	summary := f.run(t, `DELETE FROM d22_delete_pool.events e WHERE e.level = "debug"`, []string{"/deviceId"}, 8)

	assert.Equal(t, mutate.Counts{Applied: 150}, summary.Counts)
	assert.Equal(t, 150, countOf(t, container, `SELECT VALUE COUNT(1) FROM c WHERE c.level = "info"`))
	assert.Zero(t, countOf(t, container, `SELECT VALUE COUNT(1) FROM c WHERE c.level = "debug"`))
}

func TestIntegrationDeleteOnAHierarchicalKeyAndAQuotedID(t *testing.T) {
	f := newDeleteFixture(t, "d22_delete_nested")
	key := azcosmos.PartitionKeyDefinition{Paths: []string{"/tenantId", "/userId"}, Kind: azcosmos.PartitionKeyKindMultiHash, Version: 2}
	tenants := f.create(t, "tenants", key,
		map[string]any{"id": `quote"d`, "tenantId": "t1", "userId": "u1", "state": "old"},
		map[string]any{"id": "plain", "tenantId": "t1", "userId": "u2", "state": "old"},
		map[string]any{"id": "other", "tenantId": "t2", "userId": "u1", "state": "new"},
	)

	summary := f.run(t, `DELETE FROM d22_delete_nested.tenants t WHERE t.state = "old"`, key.Paths, 2)

	assert.Equal(t, mutate.Counts{Applied: 2}, summary.Counts)
	assert.Equal(t, 1, countOf(t, tenants, "SELECT VALUE COUNT(1) FROM c"))
	_, err := tenants.ReadItem(context.Background(), azcosmos.NewPartitionKeyString("t2").AppendString("u1"), "other", nil)
	assert.NoError(t, err)
}
