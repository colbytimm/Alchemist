// Package adapter defines the backend-agnostic contract every database
// adapter implements. The TUI depends only on these interfaces — never on a
// concrete implementation such as internal/adapter/cosmos.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// CatalogAdmin creates and deletes the databases and containers a Catalog
// reads. A Connection implements it when its backend allows it; callers
// detect support with a comma-ok type assertion.
type CatalogAdmin interface {
	CreateDatabase(ctx context.Context, spec DatabaseSpec) error
	DeleteDatabase(ctx context.Context, name string) error
	CreateContainer(ctx context.Context, spec ContainerSpec) error
	DeleteContainer(ctx context.Context, path []string) error
}

// ThroughputEditor reads and replaces provisioned capacity. It is separate
// from CatalogAdmin because a backend can manage a catalog without having a
// throughput concept at all.
type ThroughputEditor interface {
	Throughput(ctx context.Context, path []string) (Throughput, error)
	SetThroughput(ctx context.Context, path []string, t Throughput) error
}

// Inspector serves the metadata behind one catalog node. A Connection
// implements it when its backend has metadata worth a screen; callers detect
// support with a comma-ok type assertion.
type Inspector interface {
	Inspect(ctx context.Context, n Node) (Details, error)
}

// Details is one node's metadata, pre-rendered into ordered sections the way
// Page pre-renders rows. The TUI lays it out without interpreting it: only
// the adapter knows what its backend's fields mean, what to call them, or
// what order they read in. Raw is the backend's own representation of the
// node.
type Details struct {
	Sections []Section
	Raw      json.RawMessage
}

// Section is one titled group. Note explains an empty Properties list — a
// resource with no throughput of its own, a value the account declined to
// serve — so a section never disappears without saying why.
type Section struct {
	Title      string
	Properties []Property
	Note       string
}

type Property struct {
	Name  string
	Value string
}

// ErrUnsupported marks a request no backend can carry out, as distinct from
// one the service refused.
var ErrUnsupported = errors.New("unsupported")

type DatabaseSpec struct {
	Name       string
	Throughput Throughput // ThroughputNone leaves it without shared throughput
}

type ContainerSpec struct {
	Database      string
	Name          string
	PartitionKeys []string // "/customerId"; more than one is a hierarchical key
	Throughput    Throughput
}

type ThroughputMode int

// ThroughputShared is a container drawing on its database's capacity.
// ThroughputNone is nothing provisioned on the resource at all: a serverless
// account, or a database leaving its containers to carry their own.
const (
	ThroughputNone ThroughputMode = iota
	ThroughputManual
	ThroughputAutoscale
	ThroughputShared
)

func (m ThroughputMode) String() string {
	switch m {
	case ThroughputManual:
		return "manual"
	case ThroughputAutoscale:
		return "autoscale"
	case ThroughputShared:
		return "shared"
	}
	return "none"
}

// Throughput is provisioned capacity. RUs is the manual rate or the autoscale
// maximum, and is meaningless in the other two modes.
type Throughput struct {
	Mode ThroughputMode
	RUs  int32
}

// Provisioned reports whether t names capacity to provision on one resource.
func (t Throughput) Provisioned() bool {
	return t.Mode == ThroughputManual || t.Mode == ThroughputAutoscale
}

// Settable returns nil when t names capacity that can be provisioned on one
// resource, and otherwise says why it cannot, wrapping ErrUnsupported.
func (t Throughput) Settable() error {
	switch {
	case t.Provisioned():
		return nil
	case t.Mode == ThroughputShared:
		return fmt.Errorf("shared capacity is provisioned on the database: %w", ErrUnsupported)
	}
	return fmt.Errorf("no capacity to provision: %w", ErrUnsupported)
}

type NodeKind string

// The node kinds a catalog can serve.
const (
	NodeDatabase  NodeKind = "database"
	NodeContainer NodeKind = "container"
	NodeField     NodeKind = "field"
)

// MetaPartitionKey is the Node.Meta key holding a container's partition key,
// e.g. "/pk". A hierarchical key holds every path, joined by
// PartitionKeyPathSeparator.
const MetaPartitionKey = "partitionKey"

// PartitionKeyPathSeparator joins the paths of a hierarchical partition key.
const PartitionKeyPathSeparator = ","

// Node is one entry in the catalog tree.
type Node struct {
	Kind NodeKind
	Name string
	// Path identifies the node within its account and must not be empty; an
	// empty path is how callers denote the tree's root.
	Path        []string
	Meta        map[string]string
	HasChildren bool
}

// PartitionKeyNodes is the metadata subtree of a container: a label leaf, then
// one leaf per key path nested under it. Every adapter serves the same shape,
// so a hierarchical key reads the same everywhere and stays legible in a pane
// too narrow for all its paths on one line.
func PartitionKeyNodes(container Node) []Node {
	joined := container.Meta[MetaPartitionKey]
	if joined == "" {
		return nil
	}
	label := fieldNode(container.Path, MetaPartitionKey)
	nodes := []Node{label}
	for _, path := range strings.Split(joined, PartitionKeyPathSeparator) {
		nodes = append(nodes, fieldNode(label.Path, path))
	}
	return nodes
}

func fieldNode(parent []string, name string) Node {
	return Node{
		Kind: NodeField,
		Name: name,
		Path: append(append([]string{}, parent...), name),
	}
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

type Stats struct {
	RequestCharge float64 // RU; 0 where the backend has no such concept
	Elapsed       time.Duration
	RowCount      int
	// LeafCharges splits RequestCharge by db.container when the page was
	// merged client-side from several containers; nil otherwise.
	LeafCharges map[string]float64
}
