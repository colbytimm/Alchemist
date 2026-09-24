//go:build integration

package cosmos_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// The emulator's fixed, publicly documented account key. Not a secret.
const wellKnownKey = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw==" // #gitleaks:allow

const (
	itDatabase  = "alchemist_it"
	itContainer = "items"
	seedCount   = 25
)

// The catalog the management test builds and tears down, kept apart from the
// query fixture so the two can run in either order.
const (
	itManagedDatabase = "alchemist_it_manage"
	itDedicated       = "shipments"
	itShared          = "returns"
)

func settings() map[string]string {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8081"
	}
	key := os.Getenv("COSMOS_KEY")
	if key == "" {
		key = wellKnownKey
	}
	return map[string]string{
		"endpoint":             endpoint,
		"key":                  key,
		"insecure_skip_verify": "true", // classic emulator image serves a self-signed cert
	}
}

func seedClient(t *testing.T) *azcosmos.Client {
	t.Helper()
	s, err := cosmos.ParseSettings(settings())
	require.NoError(t, err)
	cred, err := azcosmos.NewKeyCredential(s.Key)
	require.NoError(t, err)
	opts := &azcosmos.ClientOptions{}
	opts.Transport = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // self-signed emulator cert only
	}}
	client, err := azcosmos.NewClientWithKey(s.Endpoint, cred, opts)
	require.NoError(t, err)
	return client
}

func waitForEmulator(t *testing.T) {
	t.Helper()
	require.NoError(t, connectWithRetry(t).Close())
}

func connectWithRetry(t *testing.T) adapter.Connection {
	t.Helper()
	conn, err := cosmos.Adapter{}.Connect(context.Background(), settings())
	require.NoError(t, err)
	deadline := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = conn.Ping(ctx)
		cancel()
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("emulator not reachable: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

// seedFixture inserts seedCount items spread across three partition keys.
func seedFixture(t *testing.T, client *azcosmos.Client) {
	t.Helper()
	ctx := context.Background()
	_, err := client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: itDatabase}, nil)
	require.NoError(t, err)
	db, err := client.NewDatabase(itDatabase)
	require.NoError(t, err)
	_, err = db.CreateContainer(ctx, azcosmos.ContainerProperties{
		ID:                     itContainer,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/pk"}},
	}, nil)
	require.NoError(t, err)
	container, err := client.NewContainer(itDatabase, itContainer)
	require.NoError(t, err)
	for i := 0; i < seedCount; i++ {
		pk := fmt.Sprintf("pk-%d", i%3)
		item, err := json.Marshal(map[string]any{
			"id":   fmt.Sprintf("item-%03d", i),
			"pk":   pk,
			"n":    i,
			"meta": map[string]any{"batch": i % 2, "tags": []string{"seed"}},
		})
		require.NoError(t, err)
		_, err = container.CreateItem(ctx, azcosmos.NewPartitionKeyString(pk), item, nil)
		require.NoError(t, err)
	}
}

// itemsInPartition returns how many seeded items landed in partition "pk-<n>".
func itemsInPartition(n int) int {
	count := 0
	for i := 0; i < seedCount; i++ {
		if i%3 == n {
			count++
		}
	}
	return count
}

// freshFixture drops any fixture left by an earlier run and seeds a new one.
func freshFixture(t *testing.T, client *azcosmos.Client) {
	t.Helper()
	db, err := client.NewDatabase(itDatabase)
	require.NoError(t, err)
	_, _ = db.Delete(context.Background(), nil) // clean slate from earlier runs
	seedFixture(t, client)
	t.Cleanup(func() { _, _ = db.Delete(context.Background(), nil) })
}

type recordedRequest struct {
	method string
	path   string
	pk     string // partition key routing header; empty when the query fans out
}

func (r recordedRequest) isQuery() bool { return r.method == http.MethodPost }

func (r recordedRequest) isMetadataRead() bool {
	return r.method == http.MethodGet && strings.HasSuffix(r.path, "/colls/"+itContainer)
}

// connectThroughProxy records the adapter's traffic. A pinned and a fanned-out
// query return identical rows, so the wire is the only place the pin is visible.
func connectThroughProxy(t *testing.T) (adapter.Connection, func() []recordedRequest) {
	t.Helper()
	target, err := url.Parse(settings()["endpoint"])
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // self-signed emulator cert only
	}

	var mu sync.Mutex
	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			pk:     r.Header.Get("x-ms-documentdb-partitionkey"),
		})
		mu.Unlock()
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	raw := settings()
	raw["endpoint"] = srv.URL
	conn, err := cosmos.Adapter{}.Connect(context.Background(), raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), seen...)
	}
}

func drain(t *testing.T, conn adapter.Connection, q adapter.Query) ([]adapter.Page, int) {
	t.Helper()
	cursor, err := conn.Query(context.Background(), q)
	require.NoError(t, err)
	defer cursor.Close()
	var pages []adapter.Page
	total := 0
	for cursor.HasMore() {
		page, err := cursor.NextPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, page)
		total += len(page.Rows)
	}
	return pages, total
}

func TestIntegration(t *testing.T) {
	conn := connectWithRetry(t)
	defer conn.Close()

	freshFixture(t, seedClient(t))

	t.Run("ping", func(t *testing.T) {
		require.NoError(t, conn.Ping(context.Background()))
	})

	t.Run("catalog", func(t *testing.T) {
		ctx := context.Background()
		roots, err := conn.Catalog().Root(ctx)
		require.NoError(t, err)
		var dbNode *adapter.Node
		for i := range roots {
			if roots[i].Name == itDatabase {
				dbNode = &roots[i]
			}
		}
		require.NotNil(t, dbNode, "created database should appear in catalog")

		containers, err := conn.Catalog().Children(ctx, *dbNode)
		require.NoError(t, err)
		require.Len(t, containers, 1)
		assert.Equal(t, itContainer, containers[0].Name)
		assert.Equal(t, "/pk", containers[0].Meta[adapter.MetaPartitionKey])
	})

	t.Run("cross-partition paging", func(t *testing.T) {
		pages, total := drain(t, conn, adapter.Query{
			Text: "SELECT * FROM c", Scope: []string{itDatabase, itContainer}, PageSize: 10,
		})
		assert.Equal(t, seedCount, total, "all items across all partitions")
		assert.GreaterOrEqual(t, len(pages), 2, "PageSizeHint 10 over 25 items needs multiple pages")
		assert.Greater(t, pages[0].Stats.RequestCharge, 0.0)
	})

	t.Run("cross-partition filter", func(t *testing.T) {
		_, total := drain(t, conn, adapter.Query{
			Text: "SELECT * FROM c WHERE c.n >= 10", Scope: []string{itDatabase, itContainer}, PageSize: 10,
		})
		assert.Equal(t, seedCount-10, total, "filtered subset spans several partitions")
	})

	t.Run("single-partition pin", func(t *testing.T) {
		_, total := drain(t, conn, adapter.Query{
			Text: `SELECT * FROM c WHERE c.pk = "pk-1"`, Scope: []string{itDatabase, itContainer}, PageSize: 10,
		})
		assert.Equal(t, itemsInPartition(1), total, "only the pinned partition's items")
	})

	t.Run("sample fields", func(t *testing.T) {
		sampler, ok := conn.(adapter.FieldSampler)
		require.True(t, ok, "the cosmos connection samples fields")

		sample, err := sampler.SampleFields(context.Background(), adapter.Node{
			Kind: adapter.NodeContainer, Name: itContainer, Path: []string{itDatabase, itContainer},
		})
		require.NoError(t, err)

		paths := make([]string, 0, len(sample.Fields))
		for _, field := range sample.Fields {
			paths = append(paths, field.Path)
		}
		assert.Subset(t, paths, []string{"id", "pk", "n", "meta", "meta.batch", "meta.tags", "meta.tags[]", "_ts"})
		assert.Greater(t, sample.Stats.RequestCharge, 0.0)
		assert.LessOrEqual(t, sample.Stats.RowCount, 20)
	})

	t.Run("bad sql returns service error", func(t *testing.T) {
		cursor, err := conn.Query(context.Background(), adapter.Query{
			Text: "SELEC * FRM c", Scope: []string{itDatabase, itContainer},
		})
		require.NoError(t, err)
		_, err = cursor.NextPage(context.Background())
		require.Error(t, err)
		var respErr *azcore.ResponseError
		require.ErrorAs(t, err, &respErr)
		assert.Equal(t, http.StatusBadRequest, respErr.StatusCode)
	})
}

// TestIntegrationPartitionRouting asserts on the routing header, which is the
// only observable difference between a pinned and a fanned-out query.
func TestIntegrationPartitionRouting(t *testing.T) {
	waitForEmulator(t)
	freshFixture(t, seedClient(t))
	conn, requests := connectThroughProxy(t)

	cases := []struct {
		name     string
		text     string
		wantPK   string // empty means the query must fan out
		wantRows int
	}{
		{
			name:     "equality pins to one partition",
			text:     `SELECT * FROM c WHERE c.pk = "pk-1"`,
			wantPK:   `["pk-1"]`,
			wantRows: itemsInPartition(1),
		},
		{
			name:     "unfiltered query fans out",
			text:     "SELECT * FROM c",
			wantRows: seedCount,
		},
		{
			name:     "disjunction refuses to pin",
			text:     `SELECT * FROM c WHERE c.pk = "pk-1" OR c.n < 0`,
			wantRows: itemsInPartition(1),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(requests())
			_, total := drain(t, conn, adapter.Query{
				Text: tc.text, Scope: []string{itDatabase, itContainer}, PageSize: 100,
			})
			assert.Equal(t, tc.wantRows, total)

			var queries []recordedRequest
			for _, r := range requests()[before:] {
				if r.isQuery() {
					queries = append(queries, r)
				}
			}
			require.Len(t, queries, 1, "one query request")
			assert.Equal(t, tc.wantPK, queries[0].pk)
		})
	}
}

// TestIntegrationCachesPartitionKeyPath asserts the partition key path is read
// from the service once, however many pinnable queries run.
func TestIntegrationCachesPartitionKeyPath(t *testing.T) {
	waitForEmulator(t)
	freshFixture(t, seedClient(t))
	conn, requests := connectThroughProxy(t)

	for _, pk := range []string{"pk-1", "pk-2"} {
		drain(t, conn, adapter.Query{
			Text:     `SELECT * FROM c WHERE c.pk = "` + pk + `"`,
			Scope:    []string{itDatabase, itContainer},
			PageSize: 100,
		})
	}

	reads := 0
	for _, r := range requests() {
		if r.isMetadataRead() {
			reads++
		}
	}
	assert.Equal(t, 1, reads, "container metadata should be read once and cached")
}

func inspector(t *testing.T, conn adapter.Connection) adapter.Inspector {
	t.Helper()
	i, ok := conn.(adapter.Inspector)
	require.True(t, ok, "a cosmos connection inspects its catalog")
	return i
}

func sectionTitled(t *testing.T, details adapter.Details, title string) adapter.Section {
	t.Helper()
	for _, section := range details.Sections {
		if section.Title == title {
			return section
		}
	}
	require.Failf(t, "section missing", "no section titled %q", title)
	return adapter.Section{}
}

func propertyNamed(t *testing.T, section adapter.Section, name string) string {
	t.Helper()
	for _, property := range section.Properties {
		if property.Name == name {
			return property.Value
		}
	}
	require.Failf(t, "property missing", "no property named %q in %q", name, section.Title)
	return ""
}

// answers reports whether a section has properties, or a note saying why not.
func answers(section adapter.Section) bool {
	return len(section.Properties) > 0 || section.Note != ""
}

func inspectFixture(t *testing.T, conn adapter.Connection, kind adapter.NodeKind, path ...string) adapter.Details {
	t.Helper()
	details, err := inspector(t, conn).Inspect(context.Background(), adapter.Node{
		Kind: kind,
		Name: path[len(path)-1],
		Path: path,
	})
	require.NoError(t, err)
	return details
}

// TestIntegrationInspect asserts on what the fixture fixes — which key
// partitions the container — and only on the presence of what the service
// owns, such as byte sizes and offers, which vary by image and drift.
func TestIntegrationInspect(t *testing.T) {
	conn := connectWithRetry(t)
	t.Cleanup(func() { _ = conn.Close() })
	freshFixture(t, seedClient(t))

	container := inspectFixture(t, conn, adapter.NodeContainer, itDatabase, itContainer)
	assert.Equal(t, "/pk", propertyNamed(t, sectionTitled(t, container, "Partition key"), "Paths"))
	assert.NotEmpty(t, propertyNamed(t, sectionTitled(t, container, "Storage"), "Documents size"))
	assert.True(t, answers(sectionTitled(t, container, "Throughput")))
	assert.NotEmpty(t, propertyNamed(t, sectionTitled(t, container, "Physical partitions"), "Count"))
	assert.Contains(t, propertyNamed(t, sectionTitled(t, container, "Indexing"), "Mode"), "consistent")
	assert.True(t, json.Valid(container.Raw))

	database := inspectFixture(t, conn, adapter.NodeDatabase, itDatabase)
	assert.Equal(t, itDatabase, propertyNamed(t, sectionTitled(t, database, "Identity"), "Database"))
	assert.True(t, answers(sectionTitled(t, database, "Throughput")))
}

// TestIntegrationInspectCountsSeededDocuments checks the one storage figure
// the fixture fixes. The Linux emulator image serves a usage header of zeros
// whatever the container holds, which the test cannot tell from an adapter
// that reads the wrong key, so it skips rather than passes on that image.
func TestIntegrationInspectCountsSeededDocuments(t *testing.T) {
	conn := connectWithRetry(t)
	t.Cleanup(func() { _ = conn.Close() })
	freshFixture(t, seedClient(t))

	storage := sectionTitled(t, inspectFixture(t, conn, adapter.NodeContainer, itDatabase, itContainer), "Storage")
	documents := propertyNamed(t, storage, "Documents")
	if documents == "0" {
		t.Skip("this emulator image reports no usage figures, so the document count cannot be checked against the seed")
	}
	assert.Equal(t, strconv.Itoa(seedCount), documents)
}

// createWithOwnOffer creates spec's container so that its offer is not its
// database's. The vNext emulator numbers databases and containers from separate
// counters and keys offers by that number, so a container that draws its
// database's number takes over the database's offer. Containers with no
// throughput of their own draw numbers harmlessly, so they advance the counter
// past the database's first.
func createWithOwnOffer(t *testing.T, admin adapter.CatalogAdmin, spec adapter.ContainerSpec) {
	t.Helper()
	database, err := seedClient(t).NewDatabase(spec.Database)
	require.NoError(t, err)
	read, err := database.Read(context.Background(), nil)
	require.NoError(t, err)
	if databaseNumber, ok := emulatorNumber(read.DatabaseProperties.ResourceID); ok {
		drawContainerNumbersThrough(t, admin, database, databaseNumber)
	}
	require.NoError(t, admin.CreateContainer(context.Background(), spec))
}

func drawContainerNumbersThrough(t *testing.T, admin adapter.CatalogAdmin, database *azcosmos.DatabaseClient, last int) {
	t.Helper()
	for i := 0; ; i++ {
		spacer := adapter.ContainerSpec{Database: database.ID(), Name: fmt.Sprintf("spacer_%d", i), PartitionKeys: []string{"/id"}}
		require.NoError(t, admin.CreateContainer(context.Background(), spacer))
		container, err := database.NewContainer(spacer.Name)
		require.NoError(t, err)
		read, err := container.Read(context.Background(), nil)
		require.NoError(t, err)
		if drawn, ok := emulatorNumber(read.ContainerProperties.ResourceID); !ok || drawn >= last {
			return
		}
	}
}

// emulatorNumber reads a vNext emulator resource id, the base64 of a
// zero-padded decimal; ok is false for any other account's ids.
func emulatorNumber(resourceID string) (int, bool) {
	decoded, err := base64.StdEncoding.DecodeString(resourceID)
	if err != nil {
		return 0, false
	}
	number, err := strconv.Atoi(string(decoded))
	return number, err == nil
}

func catalogAdmin(t *testing.T, conn adapter.Connection) adapter.CatalogAdmin {
	t.Helper()
	admin, ok := conn.(adapter.CatalogAdmin)
	require.True(t, ok, "a cosmos connection manages its catalog")
	return admin
}

func throughputEditor(t *testing.T, conn adapter.Connection) adapter.ThroughputEditor {
	t.Helper()
	editor, ok := conn.(adapter.ThroughputEditor)
	require.True(t, ok, "a cosmos connection edits throughput")
	return editor
}

func nodeNames(nodes []adapter.Node) []string {
	listed := make([]string, 0, len(nodes))
	for _, node := range nodes {
		listed = append(listed, node.Name)
	}
	return listed
}

func rootNames(t *testing.T, conn adapter.Connection) []string {
	t.Helper()
	roots, err := conn.Catalog().Root(context.Background())
	require.NoError(t, err)
	return nodeNames(roots)
}

func containerNodes(t *testing.T, conn adapter.Connection, database string) []adapter.Node {
	t.Helper()
	nodes, err := conn.Catalog().Children(context.Background(), adapter.Node{
		Kind: adapter.NodeDatabase,
		Name: database,
		Path: []string{database},
	})
	require.NoError(t, err)
	return nodes
}

func nodeNamed(t *testing.T, nodes []adapter.Node, name string) adapter.Node {
	t.Helper()
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	require.Failf(t, "not in the catalog", "no node named %q among %v", name, nodeNames(nodes))
	return adapter.Node{}
}

func TestIntegrationCatalogManagement(t *testing.T) {
	conn := connectWithRetry(t)
	t.Cleanup(func() { _ = conn.Close() })
	admin, editor := catalogAdmin(t, conn), throughputEditor(t, conn)
	ctx := context.Background()

	_ = admin.DeleteDatabase(ctx, itManagedDatabase) // a leftover from an earlier run would fail the create

	autoscale := adapter.Throughput{Mode: adapter.ThroughputAutoscale, RUs: 4000}
	if err := admin.CreateDatabase(ctx, adapter.DatabaseSpec{Name: itManagedDatabase, Throughput: autoscale}); err != nil {
		t.Skipf("this emulator image refuses an autoscale database, which the rest of this test builds on: %v", err)
	}
	t.Cleanup(func() { _ = admin.DeleteDatabase(context.Background(), itManagedDatabase) })
	require.Contains(t, rootNames(t, conn), itManagedDatabase)

	manual := adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400}
	createWithOwnOffer(t, admin, adapter.ContainerSpec{
		Database:      itManagedDatabase,
		Name:          itDedicated,
		PartitionKeys: []string{"/tenantId", "/customerId"},
		Throughput:    manual,
	})
	require.NoError(t, admin.CreateContainer(ctx, adapter.ContainerSpec{
		Database:      itManagedDatabase,
		Name:          itShared,
		PartitionKeys: []string{"/tenantId"},
	}))

	nodes := containerNodes(t, conn, itManagedDatabase)
	assert.Equal(t, "/tenantId,/customerId", nodeNamed(t, nodes, itDedicated).Meta[adapter.MetaPartitionKey],
		"a hierarchical key keeps every path")

	dedicated := []string{itManagedDatabase, itDedicated}
	read, err := editor.Throughput(ctx, dedicated)
	require.NoError(t, err)
	assert.Equal(t, manual, read)

	replaced := adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 800}
	require.NoError(t, editor.SetThroughput(ctx, dedicated, replaced))
	read, err = editor.Throughput(ctx, dedicated)
	require.NoError(t, err)
	assert.Equal(t, replaced, read)

	read, err = editor.Throughput(ctx, []string{itManagedDatabase})
	require.NoError(t, err)
	assert.Equal(t, autoscale, read, "the database keeps the capacity it was created with")

	read, err = editor.Throughput(ctx, []string{itManagedDatabase, itShared})
	require.NoError(t, err)
	assert.Equal(t, adapter.Throughput{Mode: adapter.ThroughputShared}, read,
		"a container with no offer of its own draws on its database")

	require.NoError(t, admin.DeleteContainer(ctx, dedicated))
	assert.NotContains(t, nodeNames(containerNodes(t, conn, itManagedDatabase)), itDedicated)

	require.NoError(t, admin.DeleteDatabase(ctx, itManagedDatabase))
	assert.NotContains(t, rootNames(t, conn), itManagedDatabase)
}
