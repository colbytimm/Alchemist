package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var (
	_ adapter.Batcher     = (*conn)(nil)
	_ adapter.ItemDrafter = (*conn)(nil)
)

// batchElapsed is what every batch reports having taken, beyond any
// configured latency.
const batchElapsed = 5 * time.Millisecond

// charges are fixed per kind, so a test can assert a batch's total. The
// operation that fails a batch is charged failedCharge; the ones it takes
// down with it nothing.
var charges = map[adapter.OperationKind]float64{
	adapter.OperationCreate:  7,
	adapter.OperationUpsert:  8,
	adapter.OperationReplace: 10,
	adapter.OperationDelete:  7,
	adapter.OperationRead:    1,
	adapter.OperationPatch:   10,
}

const failedCharge = 1

// The system fields the store adds to every item it holds: a version tag
// no earlier write had, and the modified time, in Unix seconds, from the
// adapter's clock.
const (
	etagField = "_etag"
	tsField   = "_ts"
)

// WithItems seeds the container at path with items, each filed under the
// partition its own key values name.
func WithItems(path []string, items ...json.RawMessage) Option {
	return func(a *Adapter) {
		for _, item := range items {
			a.seed(path, item)
		}
	}
}

// WithBatchFailure fails operation k, counted from one, of every batch with
// status, whatever the store says of it.
func WithBatchFailure(k int, status int) Option {
	return func(a *Adapter) { a.batchFailure = batchFailure{operation: k, status: status} }
}

type batchFailure struct {
	operation int
	status    int
}

type storedItem struct {
	partition string
	id        string
	body      json.RawMessage
	etag      string
	modified  int64
}

// Items lists what the container at path holds, each with the version tag
// the store gave it.
func (a *Adapter) Items(path []string) []json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	var items []json.RawMessage
	for _, item := range a.items[pathText(path)] {
		items = append(items, item.body)
	}
	return items
}

func (a *Adapter) seed(path []string, body json.RawMessage) {
	partition, err := a.partitionOf(path, body)
	if err != nil {
		partition = ""
	}
	var head struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &head) // an item without an id is stored under the empty one
	a.items[pathText(path)] = append(a.items[pathText(path)], a.newVersion(partition, head.ID, body))
}

// newVersion stamps body with its system fields.
func (a *Adapter) newVersion(partition, id string, body json.RawMessage) storedItem {
	a.etags++
	etag := fmt.Sprintf(`"mock-%d"`, a.etags)
	modified := a.clock().Unix()
	stamped := withField(body, etagField, etag)
	stamped = withRawField(stamped, tsField, json.RawMessage(strconv.FormatInt(modified, 10)))
	return storedItem{partition: partition, id: id, body: stamped, etag: etag, modified: modified}
}

func (a *Adapter) partitionOf(path []string, body json.RawMessage) (string, error) {
	values, err := adapter.PartitionKeyValues(body, a.keyPaths(path))
	if err != nil {
		return "", err
	}
	return partitionText(values), nil
}

func (a *Adapter) keyPaths(path []string) []string {
	db, i, err := a.locateContainer("key paths", path)
	if err != nil {
		return nil
	}
	return a.databases[db].containers[i].partitionKeys
}

// partitionText renders key values so that 1, 1.0 and 1e0 name one
// partition, as they do in the service.
func partitionText(values []json.RawMessage) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, canonicalScalar(value))
	}
	return strings.Join(parts, "\x00")
}

func canonicalScalar(value json.RawMessage) string {
	if number, err := strconv.ParseFloat(string(value), 64); err == nil {
		return strconv.FormatFloat(number, 'g', -1, 64)
	}
	return string(value)
}

// ExecuteBatch applies b to a copy of its partition, and keeps the copy
// only when every operation succeeded.
func (c *conn) ExecuteBatch(ctx context.Context, b adapter.Batch) (adapter.BatchResult, error) {
	if err := c.a.stall(ctx, OpBatch); err != nil {
		return adapter.BatchResult{}, err
	}
	result, err := c.a.applyBatch(b)
	if err != nil {
		return adapter.BatchResult{}, err
	}
	if c.a.errOps[OpBatchUnknown] {
		return adapter.BatchResult{}, fmt.Errorf("%w: %w", &InjectedError{Op: OpBatchUnknown}, adapter.ErrWriteOutcomeUnknown)
	}
	return result, nil
}

func (a *Adapter) applyBatch(b adapter.Batch) (adapter.BatchResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, _, err := a.locateContainer("batch", b.Scope); err != nil {
		return adapter.BatchResult{}, err
	}
	run := batchRun{
		adapter:   a,
		batch:     b,
		partition: partitionText(b.PartitionKey),
		items:     slices.Clone(a.items[pathText(b.Scope)]),
		bodies:    reads(b),
	}
	results, failed, err := run.apply()
	if err != nil {
		return adapter.BatchResult{}, err
	}
	if failed < 0 {
		a.items[pathText(b.Scope)] = run.items
	}
	return adapter.BatchResult{
		Committed: failed < 0,
		Results:   results,
		Stats:     batchStats(results),
	}, nil
}

func reads(b adapter.Batch) bool {
	return slices.ContainsFunc(b.Operations, func(op adapter.Operation) bool {
		return op.Kind == adapter.OperationRead
	})
}

func batchStats(results []adapter.OperationResult) adapter.Stats {
	stats := adapter.Stats{Elapsed: batchElapsed, RowCount: len(results)}
	for _, result := range results {
		stats.RequestCharge += result.RequestCharge
	}
	return stats
}

// batchRun is one batch applied to a copy of its container's items. bodies
// is whether applied operations return what they wrote, which the service
// does for a whole batch once any operation in it reads.
type batchRun struct {
	adapter   *Adapter
	batch     adapter.Batch
	partition string
	items     []storedItem
	bodies    bool
}

// apply runs every operation and reports the index of the one that failed,
// -1 when none did. From the first failure on, nothing else is applied, and
// every other operation is reported as rolled back with it.
func (r *batchRun) apply() ([]adapter.OperationResult, int, error) {
	results := make([]adapter.OperationResult, len(r.batch.Operations))
	for i, op := range r.batch.Operations {
		status, err := r.applyOne(i, op)
		if err != nil {
			return nil, 0, err
		}
		if status >= http.StatusBadRequest {
			return rolledBack(results, i, status), i, nil
		}
		results[i] = r.applied(op, status)
	}
	return results, -1, nil
}

func rolledBack(results []adapter.OperationResult, failed, status int) []adapter.OperationResult {
	for i := range results {
		results[i] = adapter.OperationResult{Outcome: adapter.OperationSkipped, Status: statusText(http.StatusFailedDependency)}
	}
	results[failed] = adapter.OperationResult{Outcome: adapter.OperationFailed, Status: statusText(status), RequestCharge: failedCharge}
	return results
}

func (r *batchRun) applied(op adapter.Operation, status int) adapter.OperationResult {
	result := adapter.OperationResult{
		Outcome:       adapter.OperationApplied,
		Status:        statusText(status),
		RequestCharge: charges[op.Kind],
	}
	if i := r.find(r.targetID(op)); i >= 0 && op.Kind != adapter.OperationDelete {
		result.ETag = r.items[i].etag
		if r.bodies {
			result.Body = r.items[i].body
		}
	}
	return result
}

func (r *batchRun) applyOne(i int, op adapter.Operation) (int, error) {
	if failure := r.adapter.batchFailure; failure.operation == i+1 {
		return failure.status, nil
	}
	switch op.Kind {
	case adapter.OperationCreate:
		return r.create(op.Body), nil
	case adapter.OperationUpsert:
		return r.upsert(op), nil
	case adapter.OperationReplace:
		return r.replace(op), nil
	case adapter.OperationDelete:
		return r.remove(op), nil
	case adapter.OperationRead:
		return r.read(op.ID), nil
	case adapter.OperationPatch:
		return r.patch(op)
	}
	return 0, fmt.Errorf("mock: batch operation %d: unknown kind %q", i+1, op.Kind)
}

func (r *batchRun) create(body json.RawMessage) int {
	id, ok := r.ownItem(body)
	switch {
	case !ok:
		return http.StatusBadRequest
	case r.find(id) >= 0:
		return http.StatusConflict
	}
	r.items = append(r.items, r.adapter.newVersion(r.partition, id, body))
	return http.StatusCreated
}

func (r *batchRun) upsert(op adapter.Operation) int {
	id, ok := r.ownItem(op.Body)
	if !ok {
		return http.StatusBadRequest
	}
	i := r.find(id)
	if i < 0 {
		if op.IfMatch != "" {
			return http.StatusPreconditionFailed
		}
		r.items = append(r.items, r.adapter.newVersion(r.partition, id, op.Body))
		return http.StatusCreated
	}
	if stale(r.items[i], op.IfMatch) {
		return http.StatusPreconditionFailed
	}
	r.items[i] = r.adapter.newVersion(r.partition, id, op.Body)
	return http.StatusOK
}

func (r *batchRun) replace(op adapter.Operation) int {
	i, status := r.current(op)
	if i < 0 {
		return status
	}
	if id, ok := r.ownItem(op.Body); !ok || id != op.ID {
		return http.StatusBadRequest
	}
	r.items[i] = r.adapter.newVersion(r.partition, op.ID, op.Body)
	return http.StatusOK
}

func (r *batchRun) remove(op adapter.Operation) int {
	i, status := r.current(op)
	if i < 0 {
		return status
	}
	r.items = slices.Delete(r.items, i, i+1)
	return http.StatusNoContent
}

func (r *batchRun) read(id string) int {
	if r.find(id) < 0 {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// patch applies the entries in order to the item's top-level fields. The
// condition is not evaluated: the mock has no query engine.
func (r *batchRun) patch(op adapter.Operation) (int, error) {
	i, status := r.current(op)
	if i < 0 {
		return status, nil
	}
	body, status, err := patchItem(r.items[i].body, op.Body)
	if err != nil || status != http.StatusOK {
		return status, err
	}
	r.items[i] = r.adapter.newVersion(r.partition, op.ID, body)
	return http.StatusOK, nil
}

// current finds the item op names, and says why it cannot when it is missing
// or no longer the version op expects.
func (r *batchRun) current(op adapter.Operation) (int, int) {
	i := r.find(op.ID)
	switch {
	case i < 0:
		return -1, http.StatusNotFound
	case stale(r.items[i], op.IfMatch):
		return -1, http.StatusPreconditionFailed
	}
	return i, 0
}

func stale(item storedItem, ifMatch string) bool {
	return ifMatch != "" && ifMatch != item.etag
}

// ownItem reads body's id, and reports false when the body is no item of
// the batch's partition.
func (r *batchRun) ownItem(body json.RawMessage) (string, bool) {
	var head struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &head) != nil || head.ID == "" {
		return "", false
	}
	partition, err := r.adapter.partitionOf(r.batch.Scope, body)
	return head.ID, err == nil && partition == r.partition
}

func (r *batchRun) targetID(op adapter.Operation) string {
	if op.ID != "" {
		return op.ID
	}
	id, _ := r.ownItem(op.Body)
	return id
}

func (r *batchRun) find(id string) int {
	return slices.IndexFunc(r.items, func(item storedItem) bool {
		return item.partition == r.partition && item.id == id
	})
}

func statusText(code int) string {
	return strconv.Itoa(code) + " " + http.StatusText(code)
}

// DraftReplace keeps the item as it is but for its version tag, which
// becomes the condition the replace is sent on.
func (c *conn) DraftReplace(item json.RawMessage) (adapter.Operation, error) {
	var head struct {
		ID   string `json:"id"`
		ETag string `json:"_etag"`
	}
	if err := json.Unmarshal(item, &head); err != nil || head.ID == "" {
		return adapter.Operation{}, fmt.Errorf("mock: draft replace: the item has no id")
	}
	body, err := adapter.WithoutFields(item, etagField, tsField)
	if err != nil {
		return adapter.Operation{}, fmt.Errorf("mock: draft replace: %w", err)
	}
	return adapter.Operation{Kind: adapter.OperationReplace, ID: head.ID, Body: body, IfMatch: head.ETag}, nil
}

// withField sets name to the string value at the top level of body,
// keeping the rest of it as written.
func withField(body json.RawMessage, name, value string) json.RawMessage {
	encoded, _ := json.Marshal(value) // a string always marshals
	return withRawField(body, name, encoded)
}

func withRawField(body json.RawMessage, name string, value json.RawMessage) json.RawMessage {
	fields, err := topLevelFields(body)
	if err != nil {
		return body
	}
	i := slices.IndexFunc(fields, func(f field) bool { return f.name == name })
	return joinFields(setField(fields, i, field{name: name, value: value}))
}
