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

// callGauge counts the calls of one kind in flight, and the most ever at
// once.
type callGauge struct {
	mu      sync.Mutex
	current int
	highest int
	total   int
}

func (g *callGauge) begin() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current++
	g.total++
	g.highest = max(g.highest, g.current)
}

func (g *callGauge) end() {
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
	matches, err := c.a.filter(request)
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
	return &scan{a: c.a, request: request, matches: matches, offset: offset, pageSize: pageSize, more: true}, nil
}

// filter is what a scan's Filter means, from the predicates a test
// registered; with no Filter, every item matches.
func (a *Adapter) filter(request adapter.ScanRequest) (func(json.RawMessage) bool, error) {
	predicate := request.Filter.Predicate
	if predicate == "" {
		return func(json.RawMessage) bool { return true }, nil
	}
	matches, ok := a.predicates[predicate]
	if !ok {
		return nil, fmt.Errorf("mock: scan %s: predicate %q is not registered with WithPredicate", pathText(request.Container), predicate)
	}
	return matches, nil
}

// scan walks the stored items by offset. Since, Filter and Projection are
// applied to each page as it is read, so a filtered scan's positions are the
// unfiltered offsets, and stay valid across writes that replace in place.
type scan struct {
	a        *Adapter
	request  adapter.ScanRequest
	matches  func(json.RawMessage) bool
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
	stored := s.a.items[pathText(s.request.Container)]
	keys := s.a.keyPaths(s.request.Container)
	end := min(s.offset+s.pageSize, len(stored))
	page := adapter.ItemPage{RequestCharge: scanCharge}
	for _, item := range stored[min(s.offset, end):end] {
		if !s.request.Since.IsZero() && item.modified < s.request.Since.Unix() || !s.matches(item.body) {
			continue
		}
		page.Items = append(page.Items, project(item.body, keys, s.request.Projection))
	}
	s.a.mu.Unlock()
	s.offset = end
	s.more = end < len(stored)
	if s.more {
		page.Next = adapter.ScanPosition(strconv.Itoa(end))
	}
	return page, nil
}

// project reduces body to what projection asks for: its id, the values at
// its key paths where it has them, nested as they are, and its system
// fields.
func project(body json.RawMessage, keyPaths []string, projection adapter.ScanProjection) json.RawMessage {
	if projection != adapter.ScanIdentity {
		return body
	}
	var whole map[string]json.RawMessage
	if json.Unmarshal(body, &whole) != nil {
		return body
	}
	identity := map[string]any{}
	for _, name := range []string{"id", etagField, tsField} {
		if value, ok := whole[name]; ok {
			identity[name] = value
		}
	}
	for _, path := range keyPaths {
		copyPath(whole, identity, strings.Split(strings.TrimPrefix(path, "/"), "/"))
	}
	projected, _ := json.Marshal(identity) // raw values and maps of them always marshal
	return projected
}

func copyPath(from map[string]json.RawMessage, to map[string]any, names []string) {
	value, ok := from[names[0]]
	if !ok {
		return
	}
	if len(names) == 1 {
		to[names[0]] = value
		return
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(value, &nested) != nil {
		return
	}
	inner, ok := to[names[0]].(map[string]any)
	if !ok {
		inner = map[string]any{}
		to[names[0]] = inner
	}
	copyPath(nested, inner, names[1:])
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
	if err := a.store(path, partitionText(values), head.ID, item); err != nil {
		return 0, err
	}
	return charges[adapter.OperationUpsert], nil
}

// store writes item as the next version of the item called id in
// partition, or as a new one. The caller holds a.mu.
func (a *Adapter) store(path []string, partition, id string, item json.RawMessage) error {
	if _, _, err := a.locateContainer("upsert", path); err != nil {
		return err
	}
	stored := a.items[pathText(path)]
	written := a.newVersion(partition, id, item)
	i := slices.IndexFunc(stored, func(s storedItem) bool { return s.partition == partition && s.id == id })
	if i < 0 {
		a.items[pathText(path)] = append(stored, written)
	} else {
		stored[i] = written
	}
	return nil
}

// PutItem writes item into the container at path as the service would an
// upsert, with a new version and the clock's modified time, and no
// injected fault. An item missing a key value is filed under the empty
// partition, as a seeded one is.
func (a *Adapter) PutItem(path []string, item json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var head struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(item, &head) != nil || head.ID == "" {
		return fmt.Errorf("mock: put into %s: the item has no id", pathText(path))
	}
	partition, err := a.partitionOf(path, item)
	if err != nil {
		partition = ""
	}
	return a.store(path, partition, head.ID, item)
}

// DeleteItem removes the item called id under key from the container at
// path.
func (a *Adapter) DeleteItem(path []string, id string, key adapter.PartitionKey) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	partition := partitionText(key)
	stored := a.items[pathText(path)]
	i := slices.IndexFunc(stored, func(s storedItem) bool { return s.partition == partition && s.id == id })
	if i < 0 {
		return fmt.Errorf("mock: delete %s from %s: not found", id, pathText(path))
	}
	a.items[pathText(path)] = slices.Delete(slices.Clone(stored), i, i+1)
	return nil
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
