//go:build integration

package cosmos_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// wellKnownKey is the Cosmos DB emulator's fixed, publicly documented
// account key. It is not a secret.
const wellKnownKey = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw==" // #gitleaks:allow

const (
	itDatabase  = "alchemist_it"
	itContainer = "items"
	seedCount   = 25
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

// seedClient builds a raw SDK client for fixture setup/teardown, with the
// same emulator TLS exemption the adapter applies.
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

// connectWithRetry waits for the emulator to accept requests.
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

// seedFixture creates the database and container and inserts seedCount
// items spread across three partition key values.
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
			"id": fmt.Sprintf("item-%03d", i),
			"pk": pk,
			"n":  i,
		})
		require.NoError(t, err)
		_, err = container.CreateItem(ctx, azcosmos.NewPartitionKeyString(pk), item, nil)
		require.NoError(t, err)
	}
}

// drain runs q and returns every page plus the total row count.
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

	client := seedClient(t)
	db, err := client.NewDatabase(itDatabase)
	require.NoError(t, err)
	_, _ = db.Delete(context.Background(), nil) // clean slate from earlier runs
	seedFixture(t, client)
	defer func() { _, _ = db.Delete(context.Background(), nil) }()

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
		assert.Equal(t, "/pk", containers[0].Meta["partitionKey"])
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
		expected := 0
		for i := 0; i < seedCount; i++ {
			if i%3 == 1 {
				expected++
			}
		}
		_, total := drain(t, conn, adapter.Query{
			Text: `SELECT * FROM c WHERE c.pk = "pk-1"`, Scope: []string{itDatabase, itContainer}, PageSize: 10,
		})
		assert.Equal(t, expected, total, "only the pinned partition's items")
	})

	t.Run("bad sql returns service error", func(t *testing.T) {
		cursor, err := conn.Query(context.Background(), adapter.Query{
			Text: "SELEC * FRM c", Scope: []string{itDatabase, itContainer},
		})
		require.NoError(t, err)
		_, err = cursor.NextPage(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cosmos: query page:")
	})
}
