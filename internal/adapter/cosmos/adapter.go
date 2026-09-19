// Package cosmos implements the adapter contract for Azure Cosmos DB
// (NoSQL API) on the azcosmos v1.5.0 SDK. Cross-partition queries are the
// default; see docs/plan/03-cosmos-adapter.md.
package cosmos

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"

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

var ErrInvalidScope = errors.New("query requires scope [database container]")

// Adapter implements adapter.Adapter for Azure Cosmos DB.
type Adapter struct{}

func (Adapter) Name() string { return Name }

// Connect builds an authenticated gateway-mode client for one Cosmos account.
func (Adapter) Connect(_ context.Context, raw map[string]string) (adapter.Connection, error) {
	settings, err := ParseSettings(raw)
	if err != nil {
		return nil, err
	}
	client, err := newClient(settings)
	if err != nil {
		return nil, fmt.Errorf("cosmos: create client: %w", err)
	}
	return &connection{
		client:   client,
		pageSize: settings.PageSize,
		pkPaths:  map[string]string{},
	}, nil
}

func newClient(settings Settings) (*azcosmos.Client, error) {
	opts := &azcosmos.ClientOptions{}
	if settings.InsecureSkipVerify {
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

// connection is an authenticated session against one Cosmos account. The TUI
// queries it from several goroutines.
type connection struct {
	client   *azcosmos.Client
	pageSize int32

	pkPathsMu sync.Mutex
	pkPaths   map[string]string // "database/container" -> partition key path
}

func (c *connection) Catalog() adapter.Catalog { return &catalog{client: c.client} }

// Query runs q against the container named by q.Scope. It fans out across all
// partitions unless the WHERE clause pins the partition key to one literal.
func (c *connection) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	if len(q.Scope) != 2 {
		return nil, fmt.Errorf("cosmos: query scope %v: %w", q.Scope, ErrInvalidScope)
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
		pkPath := c.partitionKeyPath(ctx, container, q.Scope[0]+"/"+q.Scope[1])
		if pin, ok := PinnedKey(q.Text, pkPath); ok {
			partitionKey = azcosmos.NewPartitionKeyString(pin)
		}
	}
	pager := container.NewQueryItemsPager(q.Text, partitionKey, &azcosmos.QueryOptions{PageSizeHint: pageSize})
	return newCursor(pager), nil
}

// partitionKeyPath caches the container's partition key path under key. It
// returns "" — disabling the pin optimization — for a hierarchical key or a
// failed read, and does not cache the failure. Concurrent first callers may
// each issue the read.
func (c *connection) partitionKeyPath(ctx context.Context, container *azcosmos.ContainerClient, key string) string {
	c.pkPathsMu.Lock()
	path, cached := c.pkPaths[key]
	c.pkPathsMu.Unlock()
	if cached {
		return path
	}
	resp, err := container.Read(ctx, nil)
	if err != nil || resp.ContainerProperties == nil {
		return ""
	}
	if paths := resp.ContainerProperties.PartitionKeyDefinition.Paths; len(paths) == 1 {
		path = paths[0]
	}
	c.pkPathsMu.Lock()
	c.pkPaths[key] = path
	c.pkPathsMu.Unlock()
	return path
}

// Ping verifies the account is reachable by fetching one database page.
func (c *connection) Ping(ctx context.Context) error {
	pager := c.client.NewQueryDatabasesPager("select * from dbs d", nil)
	if _, err := pager.NextPage(ctx); err != nil {
		return wrap("ping", err)
	}
	return nil
}

// Close releases nothing; the SDK client holds no closable resources.
func (c *connection) Close() error { return nil }
