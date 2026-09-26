//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/writers"
)

// mutationDatabase holds every container these tests read and write; they
// seed it themselves, and depend on no other data in the emulator.
const mutationDatabase = "u21_mutation_it"

const archiveCheapShipped = `UPDATE ` + mutationDatabase + `.orders o
SET o.status = "archived", o.archivedAt = "2026-01-01"
WHERE o.status = "shipped" AND o.total < 50`

// mutationFixture is a connection to the emulator and a fresh scratch
// database, whose first conditional patch has been probed: an emulator
// image that does not serve one skips the file rather than failing it.
type mutationFixture struct {
	conn    adapter.Connection
	client  *azcosmos.Client
	db      *azcosmos.DatabaseClient
	scanner adapter.ItemScanner
	editor  adapter.ItemEditor
}

func newMutationFixture(t *testing.T) mutationFixture {
	t.Helper()
	conn := connectWithRetry(t)
	client := seedClient(t)
	f := mutationFixture{conn: conn, client: client, db: freshDatabase(t, client, mutationDatabase)}
	var ok bool
	f.scanner, ok = conn.(adapter.ItemScanner)
	require.True(t, ok)
	f.editor, ok = conn.(adapter.ItemEditor)
	require.True(t, ok)
	f.probe(t)
	return f
}

func (f mutationFixture) probe(t *testing.T) {
	t.Helper()
	f.create(t, "probe", singleKey("/pk"), map[string]any{"id": "p", "pk": "p", "n": 1})
	_, err := f.editor.EditItem(context.Background(), []string{mutationDatabase, "probe"}, adapter.PartitionKey{json.RawMessage(`"p"`)},
		adapter.Operation{Kind: adapter.OperationPatch, ID: "p", Body: json.RawMessage(`[{"op":"set","path":"/n","value":2}]`), Condition: "FROM c WHERE (c.n = 1)"})
	var respErr *azcore.ResponseError
	if err != nil && errors.As(err, &respErr) {
		t.Skipf("emulator image does not serve conditional patch: %d", respErr.StatusCode)
	}
	require.NoError(t, err)
}

// create makes a container keyed on key and fills it with items.
func (f mutationFixture) create(t *testing.T, name string, key azcosmos.PartitionKeyDefinition, items ...map[string]any) {
	t.Helper()
	ctx := context.Background()
	_, err := f.db.CreateContainer(ctx, azcosmos.ContainerProperties{ID: name, PartitionKeyDefinition: key}, nil)
	require.NoError(t, err)
	container, err := f.db.NewContainer(name)
	require.NoError(t, err)
	for _, item := range items {
		body, err := json.Marshal(item)
		require.NoError(t, err)
		values, err := adapter.PartitionKeyValues(body, key.Paths)
		require.NoError(t, err)
		pk := azcosmos.NewPartitionKey()
		for _, value := range values {
			var s string
			require.NoError(t, json.Unmarshal(value, &s))
			pk = pk.AppendString(s)
		}
		_, err = container.CreateItem(ctx, pk, body, nil)
		require.NoError(t, err)
	}
}

// seedOrders fills an orders container keyed on /customerId, with a mix
// of statuses and totals, some items shipped and cheap and some not, a few
// of each carrying a note.
func (f mutationFixture) seedOrders(t *testing.T) {
	t.Helper()
	statuses := []string{"open", "shipped", "cancelled"}
	var items []map[string]any
	for i := range 60 {
		item := map[string]any{
			"id":         fmt.Sprintf("o%03d", i),
			"customerId": fmt.Sprintf("c%02d", i%9),
			"status":     statuses[i%len(statuses)],
			"total":      10 + (i*7)%80,
		}
		if i%4 == 0 {
			item["note"] = "seeded"
		}
		items = append(items, item)
	}
	f.create(t, "orders", singleKey("/customerId"), items...)
}

// seedEvents fills an events container keyed on /deviceId with count
// items.
func (f mutationFixture) seedEvents(t *testing.T, count int) {
	t.Helper()
	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{"id": fmt.Sprintf("e%04d", i), "deviceId": fmt.Sprintf("d%02d", i%12), "reading": i})
	}
	f.create(t, "events", singleKey("/deviceId"), items...)
}

func (f mutationFixture) keyPaths(t *testing.T, container string) []string {
	t.Helper()
	c, err := f.client.NewContainer(mutationDatabase, container)
	require.NoError(t, err)
	properties, err := c.Read(context.Background(), nil)
	require.NoError(t, err)
	return properties.ContainerProperties.PartitionKeyDefinition.Paths
}

func (f mutationFixture) selectTargets(t *testing.T, statement string) (query.Mutation, mutate.Targets) {
	t.Helper()
	m, err := query.ParseMutation(statement)
	require.NoError(t, err)
	selection, err := mutate.Select(context.Background(), f.scanner, m, f.keyPaths(t, m.Target[1]), 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, selection.Close()) }()
	for !selection.Done() {
		_, err := selection.Next(context.Background())
		require.NoError(t, err)
	}
	return m, selection.Targets()
}

func (f mutationFixture) apply(t *testing.T, m query.Mutation, targets mutate.Targets, writerCount int) mutate.Summary {
	t.Helper()
	j := mutate.NewJob(m, targets, f.editor, writers.NewPool(writerCount, writers.SystemClock{}))
	for !j.Done() {
		_, err := j.ApplyChunk(context.Background())
		require.NoError(t, err)
	}
	summary := j.Summary()
	assert.GreaterOrEqual(t, summary.WriteCharge, 0.0, "request units are not implemented in the vNext emulator")
	return summary
}

func (f mutationFixture) update(t *testing.T, statement string, writerCount int) mutate.Summary {
	t.Helper()
	m, targets := f.selectTargets(t, statement)
	return f.apply(t, m, targets, writerCount)
}

// ids lists the ids of the items text selects from a scratch container.
func (f mutationFixture) ids(t *testing.T, container, text string) []string {
	t.Helper()
	c, err := f.client.NewContainer(mutationDatabase, container)
	require.NoError(t, err)
	var ids []string
	pager := c.NewQueryItemsPager(text, azcosmos.NewPartitionKey(), nil)
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		require.NoError(t, err)
		for _, raw := range page.Items {
			var id string
			require.NoError(t, json.Unmarshal(raw, &id))
			ids = append(ids, id)
		}
	}
	return ids
}

func (f mutationFixture) read(t *testing.T, container, id string, key azcosmos.PartitionKey) map[string]json.RawMessage {
	t.Helper()
	c, err := f.client.NewContainer(mutationDatabase, container)
	require.NoError(t, err)
	resp, err := c.ReadItem(context.Background(), key, id, nil)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Value, &fields))
	return fields
}

func TestIntegrationUpdateByQuery(t *testing.T) {
	f := newMutationFixture(t)
	f.seedOrders(t)
	cheapShipped := f.ids(t, "orders", `SELECT VALUE c.id FROM c WHERE c.status = "shipped" AND c.total < 50`)
	require.NotEmpty(t, cheapShipped)

	summary := f.update(t, archiveCheapShipped, 4)

	assert.Equal(t, mutate.Counts{Applied: len(cheapShipped)}, summary.Counts)
	assert.ElementsMatch(t, cheapShipped, f.ids(t, "orders", `SELECT VALUE c.id FROM c WHERE c.status = "archived"`),
		"archived on exactly the shipped orders under 50")
	assert.ElementsMatch(t, cheapShipped, f.ids(t, "orders", `SELECT VALUE c.id FROM c WHERE c.archivedAt = "2026-01-01"`))

	t.Run("a second run selects none", func(t *testing.T) {
		_, targets := f.selectTargets(t, archiveCheapShipped)

		assert.Empty(t, targets.Items)
	})

	t.Run("an UNSET writes only the items that have the path", func(t *testing.T) {
		m, targets := f.selectTargets(t, `UPDATE `+mutationDatabase+`.orders o UNSET o.archivedAt WHERE o.status = "archived" OR o.status = "open"`)
		assert.Len(t, targets.Items, len(cheapShipped), "items that lack it are left out")
		assert.Positive(t, targets.Unaffected)

		summary := f.apply(t, m, targets, 4)

		assert.Equal(t, len(cheapShipped), summary.Counts.Applied)
		assert.Empty(t, f.ids(t, "orders", `SELECT VALUE c.id FROM c WHERE IS_DEFINED(c.archivedAt)`))
	})

	t.Run("an item that stopped matching since the selection is skipped", func(t *testing.T) {
		m, targets := f.selectTargets(t, `UPDATE `+mutationDatabase+`.orders o SET o.flag = "late" WHERE o.status = "archived"`)
		require.NotEmpty(t, targets.Items)
		first := targets.Items[0]
		container, err := f.client.NewContainer(mutationDatabase, "orders")
		require.NoError(t, err)
		var customer string
		require.NoError(t, json.Unmarshal(first.Key[0], &customer))
		key := azcosmos.NewPartitionKeyString(customer)
		patch := azcosmos.PatchOperations{}
		patch.AppendSet("/status", "reopened")
		_, err = container.PatchItem(context.Background(), key, first.ID, patch, nil)
		require.NoError(t, err)

		summary := f.apply(t, m, targets, 1)

		assert.Equal(t, 1, summary.Counts.Changed)
		assert.Equal(t, len(targets.Items)-1, summary.Counts.Applied)
		_, flagged := f.read(t, "orders", first.ID, key)["flag"]
		assert.False(t, flagged, "the changed item is left as it was")
	})

	t.Run("an item deleted since the selection is skipped as gone", func(t *testing.T) {
		m, targets := f.selectTargets(t, `UPDATE `+mutationDatabase+`.orders o SET o.flag = "gone" WHERE o.status = "archived"`)
		require.NotEmpty(t, targets.Items)
		first := targets.Items[0]
		container, err := f.client.NewContainer(mutationDatabase, "orders")
		require.NoError(t, err)
		var customer string
		require.NoError(t, json.Unmarshal(first.Key[0], &customer))
		_, err = container.DeleteItem(context.Background(), azcosmos.NewPartitionKeyString(customer), first.ID, nil)
		require.NoError(t, err)

		summary := f.apply(t, m, targets, 1)

		assert.Equal(t, 1, summary.Counts.Gone)
		assert.Equal(t, len(targets.Items)-1, summary.Counts.Applied)
	})
}

func TestIntegrationUpdateOnAHierarchicalKeyAndOddValues(t *testing.T) {
	f := newMutationFixture(t)
	f.create(t, "tenants", azcosmos.PartitionKeyDefinition{Paths: []string{"/tenantId", "/userId"}, Kind: azcosmos.PartitionKeyKindMultiHash, Version: 2},
		map[string]any{"id": `quote"d`, "tenantId": "t1", "userId": "u1", "state": "old"},
		map[string]any{"id": "plain", "tenantId": "t1", "userId": "u2", "state": "old"},
		map[string]any{"id": "other", "tenantId": "t2", "userId": "u1", "state": "new"},
	)

	summary := f.update(t, `UPDATE `+mutationDatabase+`.tenants t SET t.state = null, t.big = 12345678901234567890 WHERE t.state = "old"`, 2)

	assert.Equal(t, mutate.Counts{Applied: 2}, summary.Counts)
	quoted := f.read(t, "tenants", `quote"d`, azcosmos.NewPartitionKeyString("t1").AppendString("u1"))
	assert.Equal(t, "null", string(quoted["state"]), "SET … = null stores null")
	assert.Contains(t, string(quoted["big"]), "12345678901234567", "the number's digits")
	untouched := f.read(t, "tenants", "other", azcosmos.NewPartitionKeyString("t2").AppendString("u1"))
	assert.Equal(t, `"new"`, string(untouched["state"]))
}

func TestIntegrationUpdateEveryItemThroughThePool(t *testing.T) {
	f := newMutationFixture(t)
	const count = 300
	f.seedEvents(t, count)

	summary := f.update(t, `UPDATE `+mutationDatabase+`.events e SET e.checked = true WHERE true`, 8)

	assert.Equal(t, mutate.Counts{Applied: count}, summary.Counts)
	assert.Len(t, f.ids(t, "events", `SELECT VALUE c.id FROM c WHERE c.checked = true`), count)
}
