// Package cosmos implements the adapter contract for Azure Cosmos DB
// (NoSQL API) on the azcosmos v1.5.0 SDK. Cross-partition queries are the
// default; see docs/plan/03-cosmos-adapter.md.
package cosmos

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Name is the registry name of this adapter.
const Name = "cosmos"

// Compile-time contract checks.
var (
	_ adapter.Adapter    = Adapter{}
	_ adapter.Connection = (*connection)(nil)
)

// Adapter implements adapter.Adapter for Azure Cosmos DB.
type Adapter struct{}

// Name returns the adapter's registry name.
func (Adapter) Name() string { return Name }

// Connect validates settings and builds an authenticated gateway-mode
// client for one Cosmos account. See ParseSettings for accepted settings.
func (Adapter) Connect(_ context.Context, raw map[string]string) (adapter.Connection, error) {
	settings, err := ParseSettings(raw)
	if err != nil {
		return nil, err
	}
	client, err := newClient(settings)
	if err != nil {
		return nil, fmt.Errorf("cosmos: create client: %w", err)
	}
	return &connection{client: client, pageSize: settings.PageSize}, nil
}

// newClient constructs the azcosmos client from validated settings.
func newClient(settings Settings) (*azcosmos.Client, error) {
	opts := &azcosmos.ClientOptions{}
	if settings.InsecureSkipVerify {
		// The emulator serves a self-signed certificate; skipping
		// verification is gated behind the explicit setting and must
		// never be used against a real account.
		opts.ClientOptions = policy.ClientOptions{
			Transport: &http.Client{Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- emulator-only, explicit opt-in
			}},
		}
	}
	if settings.ConnectionString != "" {
		return azcosmos.NewClientFromConnectionString(settings.ConnectionString, opts)
	}
	cred, err := azcosmos.NewKeyCredential(settings.Key)
	if err != nil {
		return nil, err
	}
	return azcosmos.NewClientWithKey(settings.Endpoint, cred, opts)
}

// connection is an authenticated session against one Cosmos account.
type connection struct {
	client   *azcosmos.Client
	pageSize int32
}

// Catalog returns the lazy database/container tree.
func (c *connection) Catalog() adapter.Catalog { return &catalog{client: c.client} }

// Query starts q against the container named by q.Scope and returns a
// cursor over its result pages. Queries fan out across all partitions
// unless the WHERE clause pins the partition key to a single literal.
func (c *connection) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	if len(q.Scope) != 2 {
		return nil, fmt.Errorf("cosmos: query requires scope [database container], got %v", q.Scope)
	}
	container, err := c.client.NewContainer(q.Scope[0], q.Scope[1])
	if err != nil {
		return nil, fmt.Errorf("cosmos: open container %s.%s: %w", q.Scope[0], q.Scope[1], err)
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = c.pageSize
	}
	partitionKey := azcosmos.NewPartitionKey() // empty key = native cross-partition fan-out
	if hasPinCandidate(q.Text) {
		if pin := PinnedKey(q.Text, c.partitionKeyPath(ctx, container)); pin != "" {
			partitionKey = azcosmos.NewPartitionKeyString(pin)
		}
	}
	pager := container.NewQueryItemsPager(q.Text, partitionKey, &azcosmos.QueryOptions{PageSizeHint: pageSize})
	return newCursor(pager), nil
}

// partitionKeyPath reads the container's partition key path, returning ""
// (which disables the single-partition optimization) on any failure.
func (c *connection) partitionKeyPath(ctx context.Context, container *azcosmos.ContainerClient) string {
	resp, err := container.Read(ctx, nil)
	if err != nil || resp.ContainerProperties == nil {
		return ""
	}
	if paths := resp.ContainerProperties.PartitionKeyDefinition.Paths; len(paths) == 1 {
		return paths[0]
	}
	return ""
}

// Ping verifies the account is reachable by fetching one database page.
func (c *connection) Ping(ctx context.Context) error {
	pager := c.client.NewQueryDatabasesPager("select * from dbs d", nil)
	if _, err := pager.NextPage(ctx); err != nil {
		return fmt.Errorf("cosmos: ping: %w", err)
	}
	return nil
}

// Close releases nothing; the SDK client holds no closable resources.
func (c *connection) Close() error { return nil }
