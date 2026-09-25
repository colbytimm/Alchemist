//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/clone"
)

const (
	cloneDatabase     = "alchemist_clone_it"
	cloneCopyDatabase = "alchemist_clone_it_copy"
	// clonePageSize is small, so a seeded container spans several pages.
	clonePageSize = "40"
	cloneItems    = 150
)

// cloneConnection is one account on the emulator. Two of them stand in for
// two accounts, as two profiles on one endpoint do.
func cloneConnection(t *testing.T) adapter.Connection {
	t.Helper()
	raw := settings()
	raw["page_size"] = clonePageSize
	conn, err := cosmos.Adapter{}.Connect(context.Background(), raw)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	waitForEmulator(t, conn)
	return conn
}

func cloneSource(t *testing.T, conn adapter.Connection) clone.Source {
	t.Helper()
	definitions, ok := conn.(adapter.DefinitionReader)
	require.True(t, ok)
	throughput, _ := conn.(adapter.ThroughputEditor)
	items, _ := conn.(adapter.ItemScanner)
	return clone.Source{Catalog: conn.Catalog(), Definitions: definitions, Throughput: throughput, Items: items}
}

func cloneTarget(t *testing.T, conn adapter.Connection) clone.Target {
	t.Helper()
	admin, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok)
	items, _ := conn.(adapter.ItemWriter)
	return clone.Target{Catalog: conn.Catalog(), Admin: admin, Items: items}
}

// seedCloneContainer creates name in the source database with the given key
// definition and count items, each holding a value at every key path.
func seedCloneContainer(t *testing.T, db *azcosmos.DatabaseClient, name string, key azcosmos.PartitionKeyDefinition, count int) {
	t.Helper()
	ctx := context.Background()
	_, err := db.CreateContainer(ctx, azcosmos.ContainerProperties{ID: name, PartitionKeyDefinition: key}, nil)
	require.NoError(t, err)
	container, err := db.NewContainer(name)
	require.NoError(t, err)
	for i := range count {
		tenant, user := fmt.Sprintf("t%d", i%4), fmt.Sprintf("u%d", i%7)
		body, err := json.Marshal(map[string]any{
			"id": fmt.Sprintf("i%04d", i), "customerId": tenant, "tenantId": tenant, "userId": user,
			"ttl": -1, "amount": json.Number("12345678901234567890"),
		})
		require.NoError(t, err)
		pk := azcosmos.NewPartitionKeyString(tenant)
		if len(key.Paths) > 1 {
			pk = pk.AppendString(user)
		}
		_, err = container.CreateItem(ctx, pk, body, nil)
		require.NoError(t, err)
	}
}

func singleKey(path string) azcosmos.PartitionKeyDefinition {
	return azcosmos.PartitionKeyDefinition{Paths: []string{path}}
}

func prepareClone(t *testing.T, job clone.Job, from, to adapter.Connection) (clone.Plan, error) {
	t.Helper()
	source := cloneSource(t, from)
	survey, err := clone.SurveySource(context.Background(), source, job.Source)
	require.NoError(t, err)
	return clone.Prepare(context.Background(), job, survey, source, cloneTarget(t, to))
}

// runClone runs every step, and returns the totals of every page.
func runClone(t *testing.T, plan clone.Plan) clone.Progress {
	t.Helper()
	ctx := context.Background()
	if plan.CreatesDatabase {
		require.NoError(t, plan.CreateDatabase(ctx))
	}
	var sum clone.Progress
	for i := range plan.Containers {
		require.NoError(t, plan.CreateContainer(ctx, i))
		if plan.Job.Content == clone.DefinitionOnly {
			continue
		}
		sum = addUp(sum, copyFrom(t, plan, i, ""))
	}
	return sum
}

func copyFrom(t *testing.T, plan clone.Plan, i int, from adapter.ScanPosition) clone.Progress {
	t.Helper()
	c, err := plan.Open(context.Background(), i, from)
	require.NoError(t, err)
	var sum clone.Progress
	for !c.Done() {
		progress, err := c.CopyPage(context.Background())
		require.NoError(t, err)
		sum = addUp(sum, progress)
	}
	return sum
}

func addUp(sum, p clone.Progress) clone.Progress {
	sum.Read += p.Read
	sum.Written += p.Written
	sum.Skipped += p.Skipped
	sum.ReadCharge += p.ReadCharge
	sum.WriteCharge += p.WriteCharge
	return sum
}

func countItems(t *testing.T, client *azcosmos.Client, database, container string) int {
	t.Helper()
	c, err := client.NewContainer(database, container)
	require.NoError(t, err)
	pager := c.NewQueryItemsPager("SELECT VALUE COUNT(1) FROM c", azcosmos.NewPartitionKey(), nil)
	total := 0
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		require.NoError(t, err)
		for _, item := range page.Items {
			var n int
			require.NoError(t, json.Unmarshal(item, &n))
			total += n
		}
	}
	return total
}

func readProperties(t *testing.T, client *azcosmos.Client, database, container string) azcosmos.ContainerProperties {
	t.Helper()
	c, err := client.NewContainer(database, container)
	require.NoError(t, err)
	resp, err := c.Read(context.Background(), nil)
	require.NoError(t, err)
	return *resp.ContainerProperties
}

func cloneJob(source, target []string) clone.Job {
	return clone.Job{
		Source:  clone.Endpoint{Account: "a", Path: source},
		Target:  clone.Endpoint{Account: "b", Path: target},
		Content: clone.DefinitionAndItems,
		Writers: 4,
	}
}

func TestIntegrationCloneContainer(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "orders", singleKey("/customerId"), cloneItems)

	plan, err := prepareClone(t, cloneJob([]string{cloneDatabase, "orders"}, []string{cloneDatabase, "orders-copy"}), conn, conn)
	require.NoError(t, err)
	sum := runClone(t, plan)

	source := readProperties(t, client, cloneDatabase, "orders")
	copied := readProperties(t, client, cloneDatabase, "orders-copy")
	assert.Equal(t, source.PartitionKeyDefinition, copied.PartitionKeyDefinition)
	wantIndexing, err := json.Marshal(source.IndexingPolicy)
	require.NoError(t, err)
	gotIndexing, err := json.Marshal(copied.IndexingPolicy)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantIndexing), string(gotIndexing))
	assert.Equal(t, cloneItems, countItems(t, client, cloneDatabase, "orders-copy"))
	assert.Equal(t, cloneItems, sum.Written)
	assert.Positive(t, sum.ReadCharge)
	assert.Positive(t, sum.WriteCharge)

	sample := func(container string) json.RawMessage {
		c, err := client.NewContainer(cloneDatabase, container)
		require.NoError(t, err)
		resp, err := c.ReadItem(context.Background(), azcosmos.NewPartitionKeyString("t1"), "i0001", nil)
		require.NoError(t, err)
		body, err := clone.StripSystemFields(resp.Value)
		require.NoError(t, err)
		return body
	}
	assert.JSONEq(t, string(sample("orders")), string(sample("orders-copy")))
}

func TestIntegrationCloneHierarchicalKey(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	key := azcosmos.PartitionKeyDefinition{Kind: azcosmos.PartitionKeyKindMultiHash, Paths: []string{"/tenantId", "/userId"}, Version: 2}
	seedCloneContainer(t, db, "sessions", key, 60)

	plan, err := prepareClone(t, cloneJob([]string{cloneDatabase, "sessions"}, []string{cloneDatabase, "sessions-copy"}), conn, conn)
	require.NoError(t, err)
	runClone(t, plan)

	copied := readProperties(t, client, cloneDatabase, "sessions-copy")
	assert.Equal(t, azcosmos.PartitionKeyKindMultiHash, copied.PartitionKeyDefinition.Kind)
	assert.Equal(t, []string{"/tenantId", "/userId"}, copied.PartitionKeyDefinition.Paths)
	container, err := client.NewContainer(cloneDatabase, "sessions-copy")
	require.NoError(t, err)
	for i := range 60 {
		pk := azcosmos.NewPartitionKeyString(fmt.Sprintf("t%d", i%4)).AppendString(fmt.Sprintf("u%d", i%7))
		_, err := container.ReadItem(context.Background(), pk, fmt.Sprintf("i%04d", i), nil)
		require.NoError(t, err, "item %d under its full key", i)
	}
}

func TestIntegrationCloneDefinitionOnly(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "orders", singleKey("/customerId"), 10)
	job := cloneJob([]string{cloneDatabase, "orders"}, []string{cloneDatabase, "orders-empty"})
	job.Content = clone.DefinitionOnly

	plan, err := prepareClone(t, job, conn, conn)
	require.NoError(t, err)
	runClone(t, plan)

	assert.Equal(t, 0, countItems(t, client, cloneDatabase, "orders-empty"))
}

func TestIntegrationCloneDatabase(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "departments", singleKey("/tenantId"), 6)
	seedCloneContainer(t, db, "employees", singleKey("/tenantId"), 50)
	copyDB, err := client.NewDatabase(cloneCopyDatabase)
	require.NoError(t, err)
	_, _ = copyDB.Delete(context.Background(), nil)
	t.Cleanup(func() { _, _ = copyDB.Delete(context.Background(), nil) })

	plan, err := prepareClone(t, cloneJob([]string{cloneDatabase}, []string{cloneCopyDatabase}), conn, conn)
	require.NoError(t, err)
	runClone(t, plan)

	assert.Equal(t, 6, countItems(t, client, cloneCopyDatabase, "departments"))
	assert.Equal(t, 50, countItems(t, client, cloneCopyDatabase, "employees"))
}

func TestIntegrationCloneRefusesAnExistingTarget(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "orders", singleKey("/customerId"), 5)
	seedCloneContainer(t, db, "customers", singleKey("/customerId"), 3)

	_, err := prepareClone(t, cloneJob([]string{cloneDatabase, "orders"}, []string{cloneDatabase, "customers"}), conn, conn)

	require.ErrorIs(t, err, clone.ErrTargetExists)
	assert.Equal(t, 3, countItems(t, client, cloneDatabase, "customers"))
}

func TestIntegrationCloneResumesWithoutDuplicates(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "orders", singleKey("/customerId"), cloneItems)
	plan, err := prepareClone(t, cloneJob([]string{cloneDatabase, "orders"}, []string{cloneDatabase, "orders-copy"}), conn, conn)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, plan.CreateContainer(ctx, 0))
	c, err := plan.Open(ctx, 0, "")
	require.NoError(t, err)
	_, err = c.CopyPage(ctx)
	require.NoError(t, err)
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	_, err = c.CopyPage(stopped)
	require.True(t, errors.Is(err, context.Canceled), err)
	require.Less(t, countItems(t, client, cloneDatabase, "orders-copy"), cloneItems)

	copyFrom(t, plan, 0, c.Position())

	assert.Equal(t, cloneItems, countItems(t, client, cloneDatabase, "orders-copy"))
}

func TestIntegrationCloneAcrossAccounts(t *testing.T) {
	from, to := cloneConnection(t), cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	seedCloneContainer(t, db, "orders", singleKey("/customerId"), 45)
	copyDB, err := client.NewDatabase(cloneCopyDatabase)
	require.NoError(t, err)
	_, _ = copyDB.Delete(context.Background(), nil)
	t.Cleanup(func() { _, _ = copyDB.Delete(context.Background(), nil) })
	job := cloneJob([]string{cloneDatabase, "orders"}, []string{cloneCopyDatabase, "orders"})
	job.Fidelity = adapter.DefinitionPortable

	plan, err := prepareClone(t, job, from, to)
	require.NoError(t, err)
	runClone(t, plan)

	assert.True(t, plan.CreatesDatabase)
	assert.Equal(t, 45, countItems(t, client, cloneCopyDatabase, "orders"))
}

func TestIntegrationCloneFullFidelity(t *testing.T) {
	conn := cloneConnection(t)
	client := seedClient(t)
	db := freshDatabase(t, client, cloneDatabase)
	ttl := int32(3600)
	_, err := db.CreateContainer(context.Background(), azcosmos.ContainerProperties{
		ID:                     "timed",
		PartitionKeyDefinition: singleKey("/customerId"),
		DefaultTimeToLive:      &ttl,
		UniqueKeyPolicy:        &azcosmos.UniqueKeyPolicy{UniqueKeys: []azcosmos.UniqueKey{{Paths: []string{"/email"}}}},
	}, nil)
	require.NoError(t, err)
	job := cloneJob([]string{cloneDatabase, "timed"}, []string{cloneDatabase, "timed-copy"})
	job.Content = clone.DefinitionOnly

	plan, err := prepareClone(t, job, conn, conn)
	require.NoError(t, err)
	if err := plan.CreateContainer(context.Background(), 0); err != nil {
		t.Skipf("this emulator image refuses the full definition: %v", err)
	}

	copied := readProperties(t, client, cloneDatabase, "timed-copy")
	require.NotNil(t, copied.DefaultTimeToLive)
	assert.Equal(t, ttl, *copied.DefaultTimeToLive)
	require.NotNil(t, copied.UniqueKeyPolicy)
	assert.Equal(t, []string{"/email"}, copied.UniqueKeyPolicy.UniqueKeys[0].Paths)
}
