// Package mock provides a deterministic in-memory adapter used by TUI and
// engine tests. It serves a catalog its own connections may change, and
// canned multi-page query results, with options to inject errors and latency
// per operation.
package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Name is the registry name of the mock adapter.
const Name = "mock"

// Operation names accepted by WithError.
const (
	OpConnect         = "connect"
	OpPing            = "ping"
	OpQuery           = "query"
	OpNextPage        = "next_page"
	OpRoot            = "root"
	OpChildren        = "children"
	OpCreateDatabase  = "create_database"
	OpDeleteDatabase  = "delete_database"
	OpCreateContainer = "create_container"
	OpDeleteContainer = "delete_container"
	OpThroughput      = "throughput"
	OpSetThroughput   = "set_throughput"
)

var (
	_ adapter.Adapter          = (*Adapter)(nil)
	_ adapter.Connection       = (*conn)(nil)
	_ adapter.Catalog          = (*catalog)(nil)
	_ adapter.Cursor           = (*cursor)(nil)
	_ adapter.CatalogAdmin     = (*conn)(nil)
	_ adapter.ThroughputEditor = (*conn)(nil)
)

// rowsPerPage is the fixed number of rows in every canned result page.
const rowsPerPage = 10

// InjectedError is what an operation named by WithError returns.
type InjectedError struct {
	Op string
}

func (e *InjectedError) Error() string { return "mock: injected " + e.Op + " error" }

type container struct {
	name          string
	partitionKeys []string
	throughput    adapter.Throughput
}

type database struct {
	name       string
	containers []container
	throughput adapter.Throughput
}

// newFixture is the catalog a mock adapter starts from. Each adapter gets a
// copy of its own, so one adapter's mutations stay invisible to the next.
func newFixture() []database {
	manual := adapter.Throughput{Mode: adapter.ThroughputManual, RUs: 400}
	return []database{
		{name: "sales", containers: []container{
			{name: "orders", partitionKeys: []string{"/customerId"}, throughput: manual},
			{name: "customers", partitionKeys: []string{"/region"}, throughput: manual},
		}},
		{name: "telemetry", containers: []container{
			{name: "events", partitionKeys: []string{"/deviceId"}, throughput: manual},
			{name: "devices", partitionKeys: []string{"/deviceId"}, throughput: manual},
			{name: "alerts", partitionKeys: []string{"/severity"}, throughput: manual},
		}},
	}
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

	mu        sync.Mutex
	databases []database
}

// New builds a mock adapter with the given options applied.
func New(opts ...Option) *Adapter {
	a := &Adapter{errOps: map[string]bool{}, pages: 3, databases: newFixture()}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

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

type conn struct {
	a *Adapter
}

func (c *conn) Catalog() adapter.Catalog { return &catalog{a: c.a} }

// Query returns a canned multi-page cursor for any query text.
func (c *conn) Query(ctx context.Context, q adapter.Query) (adapter.Cursor, error) {
	if err := c.a.stall(ctx, OpQuery); err != nil {
		return nil, err
	}
	return &cursor{a: c.a, query: q, remaining: c.a.pages}, nil
}

func (c *conn) Ping(ctx context.Context) error { return c.a.stall(ctx, OpPing) }

// Close releases nothing; the mock holds no resources.
func (c *conn) Close() error { return nil }

func (c *conn) CreateDatabase(ctx context.Context, spec adapter.DatabaseSpec) error {
	if err := c.a.stall(ctx, OpCreateDatabase); err != nil {
		return err
	}
	return c.a.addDatabase(spec)
}

func (c *conn) DeleteDatabase(ctx context.Context, name string) error {
	if err := c.a.stall(ctx, OpDeleteDatabase); err != nil {
		return err
	}
	return c.a.removeDatabase(name)
}

func (c *conn) CreateContainer(ctx context.Context, spec adapter.ContainerSpec) error {
	if err := c.a.stall(ctx, OpCreateContainer); err != nil {
		return err
	}
	return c.a.addContainer(spec)
}

func (c *conn) DeleteContainer(ctx context.Context, path []string) error {
	if err := c.a.stall(ctx, OpDeleteContainer); err != nil {
		return err
	}
	return c.a.removeContainer(path)
}

func (c *conn) Throughput(ctx context.Context, path []string) (adapter.Throughput, error) {
	if err := c.a.stall(ctx, OpThroughput); err != nil {
		return adapter.Throughput{}, err
	}
	return c.a.readThroughput(path)
}

func (c *conn) SetThroughput(ctx context.Context, path []string, t adapter.Throughput) error {
	if err := t.Settable(); err != nil {
		return fmt.Errorf("mock: set throughput %s: %w", pathText(path), err)
	}
	if err := c.a.stall(ctx, OpSetThroughput); err != nil {
		return err
	}
	return c.a.writeThroughput(path, t)
}

func (a *Adapter) addDatabase(spec adapter.DatabaseSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.indexOf(spec.Name) >= 0 {
		return fmt.Errorf("mock: create database %q: already exists", spec.Name)
	}
	a.databases = append(a.databases, database{name: spec.Name, throughput: spec.Throughput})
	return nil
}

func (a *Adapter) removeDatabase(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.indexOf(name)
	if i < 0 {
		return fmt.Errorf("mock: delete database %q: no such database", name)
	}
	a.databases = slices.Delete(a.databases, i, i+1)
	return nil
}

func (a *Adapter) addContainer(spec adapter.ContainerSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.indexOf(spec.Database)
	if i < 0 {
		return fmt.Errorf("mock: create container %s.%s: no such database", spec.Database, spec.Name)
	}
	if containerIndex(a.databases[i].containers, spec.Name) >= 0 {
		return fmt.Errorf("mock: create container %s.%s: already exists", spec.Database, spec.Name)
	}
	a.databases[i].containers = append(a.databases[i].containers, container{
		name:          spec.Name,
		partitionKeys: spec.PartitionKeys,
		throughput:    spec.Throughput,
	})
	return nil
}

func (a *Adapter) removeContainer(path []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, i, err := a.locateContainer("delete container", path)
	if err != nil {
		return err
	}
	a.databases[db].containers = slices.Delete(a.databases[db].containers, i, i+1)
	return nil
}

func (a *Adapter) readThroughput(path []string) (adapter.Throughput, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(path) == 1 {
		i := a.indexOf(path[0])
		if i < 0 {
			return adapter.Throughput{}, fmt.Errorf("mock: throughput %s: no such database", pathText(path))
		}
		return a.databases[i].throughput, nil
	}
	db, i, err := a.locateContainer("throughput", path)
	if err != nil {
		return adapter.Throughput{}, err
	}
	return a.databases[db].containers[i].throughput, nil
}

func (a *Adapter) writeThroughput(path []string, t adapter.Throughput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(path) == 1 {
		i := a.indexOf(path[0])
		if i < 0 {
			return fmt.Errorf("mock: set throughput %s: no such database", pathText(path))
		}
		a.databases[i].throughput = t
		return nil
	}
	db, i, err := a.locateContainer("set throughput", path)
	if err != nil {
		return err
	}
	a.databases[db].containers[i].throughput = t
	return nil
}

// locateContainer resolves a [database, container] path, reporting what op
// could not find. Callers hold the lock.
func (a *Adapter) locateContainer(op string, path []string) (db, index int, err error) {
	if len(path) != 2 {
		return 0, 0, fmt.Errorf("mock: %s %s: path must name a database and a container", op, pathText(path))
	}
	db = a.indexOf(path[0])
	if db < 0 {
		return 0, 0, fmt.Errorf("mock: %s %s: no such database", op, pathText(path))
	}
	index = containerIndex(a.databases[db].containers, path[1])
	if index < 0 {
		return 0, 0, fmt.Errorf("mock: %s %s: no such container", op, pathText(path))
	}
	return db, index, nil
}

// indexOf locates a database by name. Callers hold the lock.
func (a *Adapter) indexOf(name string) int {
	for i, db := range a.databases {
		if db.name == name {
			return i
		}
	}
	return -1
}

func containerIndex(containers []container, name string) int {
	for i, c := range containers {
		if c.name == name {
			return i
		}
	}
	return -1
}

func pathText(path []string) string { return strings.Join(path, ".") }

type catalog struct {
	a *Adapter
}

func (cat *catalog) Root(ctx context.Context) ([]adapter.Node, error) {
	if err := cat.a.stall(ctx, OpRoot); err != nil {
		return nil, err
	}
	cat.a.mu.Lock()
	defer cat.a.mu.Unlock()
	nodes := make([]adapter.Node, 0, len(cat.a.databases))
	for _, db := range cat.a.databases {
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
		return adapter.PartitionKeyNodes(n), nil
	default:
		return nil, nil
	}
}

func (cat *catalog) containers(n adapter.Node) ([]adapter.Node, error) {
	cat.a.mu.Lock()
	defer cat.a.mu.Unlock()
	i := cat.a.indexOf(n.Name)
	if i < 0 {
		return nil, fmt.Errorf("mock: list containers of %q: no such database", n.Name)
	}
	containers := cat.a.databases[i].containers
	nodes := make([]adapter.Node, 0, len(containers))
	for _, c := range containers {
		nodes = append(nodes, adapter.Node{
			Kind: adapter.NodeContainer,
			Name: c.name,
			Path: []string{n.Name, c.name},
			Meta: map[string]string{
				adapter.MetaPartitionKey: strings.Join(c.partitionKeys, adapter.PartitionKeyPathSeparator),
			},
			HasChildren: true,
		})
	}
	return nodes, nil
}

// cursor pages through canned rows.
type cursor struct {
	a         *Adapter
	query     adapter.Query
	page      int
	remaining int
}

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

func (c *cursor) HasMore() bool { return c.remaining > 0 }

// Close releases nothing; the mock holds no resources.
func (c *cursor) Close() error { return nil }
