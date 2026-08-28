// Package adapter defines the backend-agnostic contract every database
// adapter implements. The TUI depends only on these interfaces — never on a
// concrete implementation such as internal/adapter/cosmos.
package adapter

import (
	"context"
	"encoding/json"
	"time"
)

// Adapter creates connections for one backend kind (e.g. "cosmos").
type Adapter interface {
	// Name returns the unique registry name of this adapter.
	Name() string
	// Connect opens an authenticated session using adapter-specific settings.
	Connect(ctx context.Context, settings map[string]string) (Connection, error)
}

// Connection is an authenticated session against one account.
type Connection interface {
	// Catalog returns the lazy metadata tree for this connection.
	Catalog() Catalog
	// Query executes q and returns a cursor over its result pages.
	Query(ctx context.Context, q Query) (Cursor, error)
	// Ping verifies the connection is alive.
	Ping(ctx context.Context) error
	// Close releases all resources held by the connection.
	Close() error
}

// Query is one executable statement plus the scope it targets.
type Query struct {
	Text     string   // adapter-native SQL, already scope-stripped
	Scope    []string // e.g. ["mydb", "orders"]
	PageSize int32
}

// Catalog is a lazy tree: databases → containers → metadata leaves.
type Catalog interface {
	// Root returns the top-level nodes (e.g. databases).
	Root(ctx context.Context) ([]Node, error)
	// Children returns the child nodes of n, loading them on demand.
	Children(ctx context.Context, n Node) ([]Node, error)
}

// Node is one entry in the catalog tree.
type Node struct {
	Kind        string // "database" | "container" | "field"
	Name        string
	Path        []string
	Meta        map[string]string // e.g. "partitionKey": "/pk"
	HasChildren bool
}

// Cursor streams result pages; the TUI wraps NextPage in a tea.Cmd.
type Cursor interface {
	// NextPage fetches the next page of results.
	NextPage(ctx context.Context) (Page, error)
	// HasMore reports whether another page is available.
	HasMore() bool
	// Close releases resources held by the cursor.
	Close() error
}

// Page is one fetched page of query results.
type Page struct {
	Columns []string          // stable union of item keys, first-page order locked
	Rows    [][]string        // pre-rendered cells
	Raw     []json.RawMessage // original items, for export / detail view
	Stats   Stats
}

// Stats describes the cost and shape of one fetched page.
type Stats struct {
	RequestCharge float64 // RU; 0 where the backend has no such concept
	Elapsed       time.Duration
	RowCount      int
	More          bool
}
