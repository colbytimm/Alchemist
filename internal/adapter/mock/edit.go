package mock

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.ItemEditor = (*conn)(nil)

// OpEdit fails every edit, before anything is applied, when given to
// WithError.
const OpEdit = "edit"

// WithPredicate registers what predicate means, as the function a test
// writes in Go: the mock parses no SQL. A scan filtered by predicate, and an
// edit conditioned on it, both ask matches, so the selection and the check
// each write makes agree the way the service's would.
func WithPredicate(predicate string, matches func(item json.RawMessage) bool) Option {
	return func(a *Adapter) { a.predicates[predicate] = matches }
}

// WithEditConflict makes the first edit of the item called id fail as
// changed since it was selected, leaving the item as it is.
func WithEditConflict(id string) Option {
	return func(a *Adapter) { a.edit.conflicts[id] = true }
}

// WithEditRefusal makes the first edit of the item called id fail with an
// InjectedError, as a refusal the service gave before applying anything.
func WithEditRefusal(id string) Option {
	return func(a *Adapter) { a.edit.refusals[id] = true }
}

// WithEditUnknown makes the first edit of the item called id apply, then
// report no answer, as a write whose response was lost.
func WithEditUnknown(id string) Option {
	return func(a *Adapter) { a.edit.unknowns[id] = true }
}

// WithEditThrottle makes the first times edits of the item called id fail
// as throttled, naming retryAfter as the wait.
func WithEditThrottle(id string, times int, retryAfter time.Duration) Option {
	return func(a *Adapter) { a.edit.throttles[id] = throttle{remaining: times, retryAfter: retryAfter} }
}

// editFaults are the injected outcomes of edits, keyed by item id. Guarded
// by Adapter.mu.
type editFaults struct {
	throttles map[string]throttle
	conflicts map[string]bool
	refusals  map[string]bool
	unknowns  map[string]bool
}

func (a *Adapter) HighestConcurrentEdits() int {
	a.edits.mu.Lock()
	defer a.edits.mu.Unlock()
	return a.edits.highest
}

// EditedIDs lists the id of every edit that reached the mock, in the order
// they did, throttled and refused ones included.
func (a *Adapter) EditedIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.edited)
}

func (c *conn) EditItem(ctx context.Context, container []string, key adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	c.a.edits.begin()
	defer c.a.edits.end()
	if err := c.a.stall(ctx, OpEdit); err != nil {
		return adapter.OperationResult{}, err
	}
	return c.a.applyEdit(container, key, op)
}

func (a *Adapter) applyEdit(container []string, key adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.edited = append(a.edited, op.ID)
	e := itemEdit{adapter: a, container: container, partition: partitionText(key), op: op}
	return e.apply()
}

// itemEdit is one edit, applied under Adapter.mu.
type itemEdit struct {
	adapter   *Adapter
	container []string
	partition string
	op        adapter.Operation
}

func (e itemEdit) apply() (adapter.OperationResult, error) {
	if _, _, err := e.adapter.locateContainer("edit", e.container); err != nil {
		return adapter.OperationResult{}, err
	}
	if e.op.Kind != adapter.OperationPatch && e.op.Kind != adapter.OperationDelete {
		return adapter.OperationResult{}, e.fail(adapter.ErrUnsupported)
	}
	if result, err := e.injected(); err != nil {
		return result, err
	}
	i := e.find()
	if i < 0 {
		return refused(http.StatusNotFound), e.fail(adapter.ErrItemNotFound)
	}
	holds, err := e.holds(e.adapter.items[pathText(e.container)][i])
	if err != nil {
		return refused(http.StatusBadRequest), e.fail(err)
	}
	if !holds {
		return refused(http.StatusPreconditionFailed), e.fail(adapter.ErrPreconditionFailed)
	}
	result, err := e.write(i)
	if err != nil {
		return result, err
	}
	if e.adapter.edit.unknowns[e.op.ID] {
		delete(e.adapter.edit.unknowns, e.op.ID)
		return adapter.OperationResult{}, fmt.Errorf("%w: %w", &InjectedError{Op: OpEdit}, adapter.ErrWriteOutcomeUnknown)
	}
	return result, nil
}

// injected answers for a fault a test asked for, before the store is read,
// and is nil when there is none.
func (e itemEdit) injected() (adapter.OperationResult, error) {
	faults := e.adapter.edit
	id := e.op.ID
	if t, ok := faults.throttles[id]; ok && t.remaining > 0 {
		t.remaining--
		faults.throttles[id] = t
		return refused(http.StatusTooManyRequests), &adapter.ThrottledError{RetryAfter: t.retryAfter, Err: &InjectedError{Op: OpEdit}}
	}
	if faults.conflicts[id] {
		delete(faults.conflicts, id)
		return refused(http.StatusPreconditionFailed), e.fail(adapter.ErrPreconditionFailed)
	}
	if faults.refusals[id] {
		delete(faults.refusals, id)
		return refused(http.StatusBadRequest), &InjectedError{Op: OpEdit}
	}
	return adapter.OperationResult{}, nil
}

func (e itemEdit) find() int {
	return slices.IndexFunc(e.adapter.items[pathText(e.container)], func(item storedItem) bool {
		return item.partition == e.partition && item.id == e.op.ID
	})
}

// holds reports whether the item is still what the operation expects: the
// version its IfMatch names, and a match for its condition.
func (e itemEdit) holds(item storedItem) (bool, error) {
	if stale(item, e.op.IfMatch) {
		return false, nil
	}
	return e.adapter.conditionHolds(e.op.Condition, item.body)
}

func (e itemEdit) write(i int) (adapter.OperationResult, error) {
	path := pathText(e.container)
	stored := e.adapter.items[path]
	if e.op.Kind == adapter.OperationDelete {
		e.adapter.items[path] = slices.Delete(slices.Clone(stored), i, i+1)
		return applied(http.StatusNoContent, e.op.Kind, ""), nil
	}
	body, status, err := patchItem(stored[i].body, e.op.Body)
	switch {
	case err != nil:
		return refused(http.StatusBadRequest), e.fail(err)
	case status != http.StatusOK:
		return refused(status), e.fail(fmt.Errorf("the patch was refused with %s", statusText(status)))
	}
	stored[i] = e.adapter.newVersion(e.partition, e.op.ID, body)
	return applied(http.StatusOK, e.op.Kind, stored[i].etag), nil
}

func (e itemEdit) fail(err error) error {
	return fmt.Errorf("mock: %s %s in %s: %w", e.op.Kind, e.op.ID, pathText(e.container), err)
}

func refused(status int) adapter.OperationResult {
	return adapter.OperationResult{Outcome: adapter.OperationFailed, Status: statusText(status), RequestCharge: failedCharge}
}

func applied(status int, kind adapter.OperationKind, etag string) adapter.OperationResult {
	return adapter.OperationResult{Outcome: adapter.OperationApplied, Status: statusText(status), ETag: etag, RequestCharge: charges[kind]}
}

// conditionHolds evaluates a patch condition of the shapes the mock reads:
// FROM <alias> WHERE (<predicate>), then AND <guard> once for each path
// that must be there, or FROM <alias> WHERE <guard> AND ... alone. A guard
// is IS_DEFINED, IS_OBJECT or IS_ARRAY of a path written with dots. The
// predicate is one a test registered. What the vNext emulator refuses with
// 400, the mock refuses too: a bracket or an index in a guard, and a bare
// true beside an AND.
func (a *Adapter) conditionHolds(condition string, item json.RawMessage) (bool, error) {
	if condition == "" {
		return true, nil
	}
	alias, rest, ok := cutConditionHead(condition)
	if !ok {
		return false, fmt.Errorf("condition %q does not start FROM <alias> WHERE", condition)
	}
	predicate, tail, err := a.cutPredicate(rest)
	if err != nil {
		return false, fmt.Errorf("condition %q: %w", condition, err)
	}
	if predicate == "true" && tail != "" {
		return false, fmt.Errorf("condition %q: a bare true beside an AND", condition)
	}
	guards, err := readGuards(alias, tail)
	if err != nil {
		return false, fmt.Errorf("condition %q: %w", condition, err)
	}
	matches := a.predicates[predicate]
	if predicate == "" {
		matches = func(json.RawMessage) bool { return true }
	}
	return matches(item) && everyGuardHolds(item, guards), nil
}

func cutConditionHead(condition string) (alias, rest string, ok bool) {
	rest, ok = strings.CutPrefix(condition, "FROM ")
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, " WHERE ")
}

// cutPredicate reads the registered predicate a condition opens with, in
// its parentheses; a condition of guards alone has none, and holds by
// them alone.
func (a *Adapter) cutPredicate(rest string) (string, string, error) {
	if !strings.HasPrefix(rest, "(") {
		return "", " AND " + rest, nil
	}
	for _, predicate := range longestFirst(a.predicates) {
		if tail, found := strings.CutPrefix(rest, "("+predicate+")"); found {
			return predicate, tail, nil
		}
	}
	return "", "", errors.New("it names no predicate registered with WithPredicate")
}

// longestFirst orders the predicates so that one which starts another is
// tried after it.
func longestFirst(predicates map[string]func(json.RawMessage) bool) []string {
	return slices.SortedFunc(maps.Keys(predicates), func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), cmp.Compare(a, b))
	})
}

// guard is one check of a condition: its function and the path it asks
// about.
type guard struct {
	function string
	path     []string
}

var guardFunctions = []string{"IS_DEFINED", "IS_OBJECT", "IS_ARRAY"}

// readGuards reads the guards after a condition's predicate.
func readGuards(alias, tail string) ([]guard, error) {
	var guards []guard
	for tail != "" {
		clause, found := strings.CutPrefix(tail, " AND ")
		function, ref, opened := strings.Cut(clause, "("+alias)
		end := strings.Index(ref, ")")
		if !found || !opened || end < 0 || !slices.Contains(guardFunctions, function) {
			return nil, fmt.Errorf("unexpected %q", tail)
		}
		if strings.ContainsAny(ref[:end], "[]") {
			return nil, fmt.Errorf("%s of %q: a bracket in a patch condition", function, ref[:end])
		}
		guards = append(guards, guard{function: function, path: strings.Split(strings.TrimPrefix(ref[:end], "."), ".")})
		tail = ref[end+1:]
	}
	return guards, nil
}

func everyGuardHolds(item json.RawMessage, guards []guard) bool {
	for _, g := range guards {
		value, ok := valueAtPath(item, g.path)
		switch {
		case !ok:
			return false
		case g.function == "IS_OBJECT" && (len(value) == 0 || value[0] != '{'):
			return false
		case g.function == "IS_ARRAY" && (len(value) == 0 || value[0] != '['):
			return false
		}
	}
	return true
}

// valueAtPath is what item holds at the property names of path.
func valueAtPath(item json.RawMessage, path []string) (json.RawMessage, bool) {
	value := item
	for _, name := range path {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil || object == nil {
			return nil, false
		}
		next, ok := object[name]
		if !ok {
			return nil, false
		}
		value = next
	}
	return bytes.TrimSpace(value), true
}
