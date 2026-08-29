// Package mock provides a deterministic in-memory adapter used by TUI and
// engine tests. It serves a fixed catalog and canned multi-page query
// results, with options to inject errors and latency per operation.
package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Name is the registry name of the mock adapter.
const Name = "mock"

// Operation names accepted by WithError.
const (
	OpConnect  = "connect"
	OpPing     = "ping"
	OpQuery    = "query"
	OpNextPage = "next_page"
	OpRoot     = "root"
	OpChildren = "children"
)

// Compile-time contract checks for all four interfaces.
var (
	_ adapter.Adapter    = (*Adapter)(nil)
	_ adapter.Connection = (*conn)(nil)
	_ adapter.Catalog    = (*catalog)(nil)
	_ adapter.Cursor     = (*cursor)(nil)
)

// rowsPerPage is the fixed number of rows in every canned result page.
const rowsPerPage = 10

// InjectedError is what an operation named by WithError returns.
type InjectedError struct {
	Op string
}

func (e *InjectedError) Error() string { return "mock: injected " + e.Op + " error" }

// fixtureContainer is one container in the fixture catalog.
type fixtureContainer struct {
	name         string
	partitionKey string
}

// fixtureDB is one database in the fixture catalog.
type fixtureDB struct {
	name       string
	containers []fixtureContainer
}

// fixture is the deterministic catalog served by every mock connection.
var fixture = []fixtureDB{
	{name: "sales", containers: []fixtureContainer{
		{name: "orders", partitionKey: "/customerId"},
		{name: "customers", partitionKey: "/region"},
	}},
	{name: "telemetry", containers: []fixtureContainer{
		{name: "events", partitionKey: "/deviceId"},
		{name: "devices", partitionKey: "/deviceId"},
		{name: "alerts", partitionKey: "/severity"},
	}},
}

// Option configures the mock adapter.
type Option func(*Adapter)

// WithError makes the given operation (one of the Op constants) fail.
func WithError(op string) Option {
	return func(a *Adapter) { a.errOps[op] = true }
}

// WithLatency adds a fixed delay to every operation.
func WithLatency(d time.Duration) Option {
	return func(a *Adapter) { a.latency = d }
}

// WithPages sets how many result pages a query cursor returns.
func WithPages(n int) Option {
	return func(a *Adapter) { a.pages = n }
}

// Adapter is the in-memory mock implementation of adapter.Adapter.
type Adapter struct {
	errOps  map[string]bool
	latency time.Duration
	pages   int
}

// New builds a mock adapter with the given options applied.
func New(opts ...Option) *Adapter {
	a := &Adapter{errOps: map[string]bool{}, pages: 3}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the adapter's registry name.
func (a *Adapter) Name() string { return Name }

// Connect returns an in-memory connection; settings are ignored.
func (a *Adapter) Connect(ctx context.Context, _ map[string]string) (adapter.Connection, error) {
	if err := a.stall(ctx, OpConnect); err != nil {
		return nil, err
	}
	return &conn{a: a}, nil
}

// stall applies configured latency, then fails if op has an injected error.
func (a *Adapter) stall(ctx context.Context, op string) error {
	if a.latency > 0 {
		select {
		case <-time.After(a.latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.errOps[op] {
		return &InjectedError{Op: op}
	}
	return nil
}

// conn is the mock connection.
type conn struct {
	a *Adapter
}

// Catalog returns the fixture catalog.
func (c *conn) Catalog() adapter.Catalog { return &catalog{a: c.a} }

// Query returns a canned multi-page cursor for any query text.
func (c *conn) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	if err := c.a.stall(ctx, OpQuery); err != nil {
		return nil, err
	}
	return &cursor{a: c.a, query: q, remaining: c.a.pages}, nil
}

// Ping reports the connection as alive unless a ping error is injected.
func (c *conn) Ping(ctx context.Context) error { return c.a.stall(ctx, OpPing) }

// Close releases nothing; the mock holds no resources.
func (c *conn) Close() error { return nil }

// catalog serves the fixture tree.
type catalog struct {
	a *Adapter
}

// Root returns the fixture databases.
func (cat *catalog) Root(ctx context.Context) ([]adapter.Node, error) {
	if err := cat.a.stall(ctx, OpRoot); err != nil {
		return nil, err
	}
	nodes := make([]adapter.Node, 0, len(fixture))
	for _, db := range fixture {
		nodes = append(nodes, adapter.Node{
			Kind:        adapter.NodeDatabase,
			Name:        db.name,
			Path:        []string{db.name},
			HasChildren: true,
		})
	}
	return nodes, nil
}

// Children expands a database into containers, and a container into its
// metadata leaves. Field nodes have no children.
func (cat *catalog) Children(ctx context.Context, n adapter.Node) ([]adapter.Node, error) {
	if err := cat.a.stall(ctx, OpChildren); err != nil {
		return nil, err
	}
	switch n.Kind {
	case adapter.NodeDatabase:
		return cat.containers(n)
	case adapter.NodeContainer:
		return []adapter.Node{{
			Kind: adapter.NodeField,
			Name: adapter.MetaPartitionKey + " " + n.Meta[adapter.MetaPartitionKey],
			Path: append(append([]string{}, n.Path...), adapter.MetaPartitionKey),
		}}, nil
	default:
		return nil, nil
	}
}

// containers lists the fixture containers of one database node.
func (cat *catalog) containers(n adapter.Node) ([]adapter.Node, error) {
	for _, db := range fixture {
		if db.name != n.Name {
			continue
		}
		nodes := make([]adapter.Node, 0, len(db.containers))
		for _, c := range db.containers {
			nodes = append(nodes, adapter.Node{
				Kind:        adapter.NodeContainer,
				Name:        c.name,
				Path:        []string{db.name, c.name},
				Meta:        map[string]string{adapter.MetaPartitionKey: c.partitionKey},
				HasChildren: true,
			})
		}
		return nodes, nil
	}
	return nil, fmt.Errorf("mock: unknown database %q", n.Name)
}

// cursor pages through canned rows.
type cursor struct {
	a         *Adapter
	query     adapter.Query
	page      int
	remaining int
}

// NextPage returns the next canned page of ten rows.
func (c *cursor) NextPage(ctx context.Context) (adapter.Page, error) {
	if err := c.a.stall(ctx, OpNextPage); err != nil {
		return adapter.Page{}, err
	}
	if c.remaining <= 0 {
		return adapter.Page{}, errors.New("mock: no more pages")
	}
	c.page++
	c.remaining--
	page := adapter.Page{
		Columns: []string{"id", "pk", "amount", "note"},
		Stats: adapter.Stats{
			RequestCharge: 2.5,
			Elapsed:       5 * time.Millisecond,
			RowCount:      rowsPerPage,
		},
	}
	for i := 0; i < rowsPerPage; i++ {
		id := fmt.Sprintf("item-%d-%d", c.page, i)
		pk := fmt.Sprintf("pk-%d", i%3)
		amount := fmt.Sprintf("%d", (c.page*100)+i)
		note := fmt.Sprintf("row %d of page %d", i, c.page)
		page.Rows = append(page.Rows, []string{id, pk, amount, note})
		raw := fmt.Sprintf(`{"id":%q,"pk":%q,"amount":%s,"note":%q}`, id, pk, amount, note)
		page.Raw = append(page.Raw, json.RawMessage(raw))
	}
	return page, nil
}

// HasMore reports whether another canned page is available.
func (c *cursor) HasMore() bool { return c.remaining > 0 }

// Close releases nothing; the mock holds no resources.
func (c *cursor) Close() error { return nil }
