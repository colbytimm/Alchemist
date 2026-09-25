package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var (
	_ adapter.DefinitionReader = (*conn)(nil)
	_ adapter.ItemScanner      = (*conn)(nil)
	_ adapter.ItemWriter       = (*conn)(nil)
)

// Operation names WithError accepts for the calls a clone makes. OpScan
// fails a page read, not the opening of a scan.
const (
	OpDefinition = "definition"
	OpScan       = "scan"
	OpUpsert     = "upsert"
)

const scanCharge = 2.5

// defaultPolicies is the definition of a container created without one. It
// holds a policy a portable read leaves out, so the two readings differ.
var defaultPolicies = json.RawMessage(`{"indexingPolicy":{"indexingMode":"consistent","automatic":true},"vectorEmbeddingPolicy":{"vectorEmbeddings":[]}}`)

var accountBound = []string{"vectorEmbeddingPolicy", "fullTextPolicy"}

// WithItemCount seeds the container at path with count generated items,
// spread over five values of its first partition key path.
func WithItemCount(path []string, count int) Option {
	return func(a *Adapter) {
		field := "pk"
		if paths := a.keyPaths(path); len(paths) > 0 {
			field = strings.TrimPrefix(paths[0], "/")
		}
		for i := range count {
			body := fmt.Sprintf(`{"id":"item-%05d",%q:"pk-%d","n":%d}`, i, field, i%5, i)
			a.seed(path, json.RawMessage(body))
		}
	}
}

// WithThrottle makes the first times upserts of the item called id fail as
// throttled, naming retryAfter as the wait.
func WithThrottle(id string, times int, retryAfter time.Duration) Option {
	return func(a *Adapter) { a.faults.throttles[id] = throttle{remaining: times, retryAfter: retryAfter} }
}

// WithWriteError makes the first upsert of the item called id fail with an
// InjectedError; the next one succeeds.
func WithWriteError(id string) Option {
	return func(a *Adapter) { a.faults.failures[id] = true }
}

// WithUnknownSize makes every definition read say nothing about size, as an
// account serving no usage figures does.
func WithUnknownSize() Option {
	return func(a *Adapter) { a.unknownSize = true }
}

type throttle struct {
	remaining  int
	retryAfter time.Duration
}

// writeFaults are the injected failures of upserts, keyed by item id.
// Guarded by Adapter.mu.
type writeFaults struct {
	throttles map[string]throttle
	failures  map[string]bool
}

type upsertGauge struct {
	mu      sync.Mutex
	current int
	highest int
	total   int
}

func (g *upsertGauge) begin() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current++
	g.total++
	g.highest = max(g.highest, g.current)
}

func (g *upsertGauge) end() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current--
}

// HighestConcurrentUpserts is the most upserts that were ever in flight at
// once, on any connection of a.
func (a *Adapter) HighestConcurrentUpserts() int {
	a.upserts.mu.Lock()
	defer a.upserts.mu.Unlock()
	return a.upserts.highest
}

func (a *Adapter) Upserts() int {
	a.upserts.mu.Lock()
	defer a.upserts.mu.Unlock()
	return a.upserts.total
}

func (c *conn) ContainerDefinition(ctx context.Context, path []string, fidelity adapter.DefinitionFidelity) (adapter.ContainerDefinition, error) {
	if err := c.a.stall(ctx, OpDefinition); err != nil {
		return adapter.ContainerDefinition{}, err
	}
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	db, i, err := c.a.locateContainer("definition", path)
	if err != nil {
		return adapter.ContainerDefinition{}, err
	}
	stored := c.a.databases[db].containers[i]
	policies := stored.policies
	if policies == nil {
		policies = defaultPolicies
	}
	if fidelity == adapter.DefinitionPortable {
		if policies, err = adapter.WithoutFields(policies, accountBound...); err != nil {
			return adapter.ContainerDefinition{}, fmt.Errorf("mock: definition %s: %w", pathText(path), err)
		}
	}
	return adapter.ContainerDefinition{
		PartitionKeys: slices.Clone(stored.partitionKeys),
		Policies:      adapter.PolicyDocument{Backend: Name, Raw: policies},
		Size:          c.a.sizeOf(path),
	}, nil
}

func (a *Adapter) sizeOf(path []string) adapter.SizeEstimate {
	if a.unknownSize {
		return adapter.SizeEstimate{}
	}
	items := a.items[pathText(path)]
	size := adapter.SizeEstimate{Items: int64(len(items)), Known: true}
	for _, item := range items {
		size.Bytes += int64(len(item.body))
	}
	return size
}

// ScanItems pages through the container's items in the order they were
// stored. The position is the offset of the next item, in decimal.
func (c *conn) ScanItems(_ context.Context, request adapter.ScanRequest) (adapter.ItemScan, error) {
	c.a.mu.Lock()
	_, _, err := c.a.locateContainer("scan", request.Container)
	c.a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	offset := 0
	if request.From != "" {
		if offset, err = strconv.Atoi(string(request.From)); err != nil || offset < 0 {
			return nil, fmt.Errorf("mock: scan %s: position %q was not issued by this adapter", pathText(request.Container), request.From)
		}
	}
	pageSize := int(request.PageSize)
	if pageSize <= 0 {
		pageSize = rowsPerPage
	}
	return &scan{a: c.a, path: request.Container, offset: offset, pageSize: pageSize, more: true}, nil
}

type scan struct {
	a        *Adapter
	path     []string
	offset   int
	pageSize int
	more     bool
}

func (s *scan) NextPage(ctx context.Context) (adapter.ItemPage, error) {
	if err := s.a.stall(ctx, OpScan); err != nil {
		return adapter.ItemPage{}, err
	}
	if !s.more {
		return adapter.ItemPage{}, errors.New("mock: no more pages")
	}
	s.a.mu.Lock()
	stored := s.a.items[pathText(s.path)]
	end := min(s.offset+s.pageSize, len(stored))
	page := adapter.ItemPage{RequestCharge: scanCharge}
	for _, item := range stored[min(s.offset, end):end] {
		page.Items = append(page.Items, item.body)
	}
	s.a.mu.Unlock()
	s.offset = end
	s.more = end < len(stored)
	if s.more {
		page.Next = adapter.ScanPosition(strconv.Itoa(end))
	}
	return page, nil
}

func (s *scan) HasMore() bool { return s.more }

func (s *scan) Close() error { return nil }

// OpenItemSink writes into the container at path, reading its key paths
// once, as the service's own sink does.
func (c *conn) OpenItemSink(_ context.Context, path []string) (adapter.ItemSink, error) {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if _, _, err := c.a.locateContainer("open sink", path); err != nil {
		return nil, err
	}
	return &sink{a: c.a, path: slices.Clone(path), keys: c.a.keyPaths(path)}, nil
}

type sink struct {
	a    *Adapter
	path []string
	keys []string
}

func (s *sink) Upsert(ctx context.Context, item json.RawMessage) (float64, error) {
	s.a.upserts.begin()
	defer s.a.upserts.end()
	if err := s.a.stall(ctx, OpUpsert); err != nil {
		return 0, err
	}
	return s.a.upsert(s.path, s.keys, item)
}

func (a *Adapter) upsert(path, keys []string, item json.RawMessage) (float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	op := "upsert into " + pathText(path)
	var head struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(item, &head) != nil || head.ID == "" {
		return 0, fmt.Errorf("mock: %s: the item has no id: %w", op, adapter.ErrItemRefused)
	}
	if err := a.faults.take(head.ID); err != nil {
		return 0, err
	}
	values, err := adapter.PartitionKeyValues(item, keys)
	if err != nil {
		return 0, fmt.Errorf("mock: %s: %s: %w: %w", op, head.ID, adapter.ErrItemRefused, err)
	}
	if _, _, err := a.locateContainer("upsert", path); err != nil {
		return 0, err
	}
	partition := partitionText(values)
	stored := a.items[pathText(path)]
	written := a.newVersion(partition, head.ID, item)
	i := slices.IndexFunc(stored, func(s storedItem) bool { return s.partition == partition && s.id == head.ID })
	if i < 0 {
		a.items[pathText(path)] = append(stored, written)
	} else {
		stored[i] = written
	}
	return charges[adapter.OperationUpsert], nil
}

func (f writeFaults) take(id string) error {
	if f.failures[id] {
		delete(f.failures, id)
		return &InjectedError{Op: OpUpsert}
	}
	t, ok := f.throttles[id]
	if !ok || t.remaining == 0 {
		return nil
	}
	t.remaining--
	f.throttles[id] = t
	return &adapter.ThrottledError{RetryAfter: t.retryAfter, Err: &InjectedError{Op: OpUpsert}}
}
