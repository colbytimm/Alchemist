package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/canonical"
)

// The service's limits on one transactional batch.
const (
	MaxBatchOperations = 100
	MaxBatchBytes      = 2 * 1024 * 1024
)

// operationOverhead is what the size estimate allows for the envelope of
// each operation: its kind, field names and punctuation, rounded up.
const operationOverhead = 128

// nearLimit is the share of a limit past which a batch is warned about.
const nearLimit = 0.9

const bytesPerMB = 1024 * 1024

// patchOps are the operations a patch entry can name.
var patchOps = []string{"add", "set", "replace", "remove", "incr", "move"}

// BatchCheck is what validation found. Any Problem refuses the batch;
// Warnings are for the review to show. Bytes is the estimated payload.
type BatchCheck struct {
	Problems []string
	Warnings []string
	Bytes    int
}

// CheckBatch validates b against its container's partition key paths, and
// lists every problem found, not only the first.
func CheckBatch(b adapter.Batch, keyPaths []string) BatchCheck {
	c := batchChecker{batch: b, keyPaths: keyPaths}
	c.check.Bytes = EstimateBytes(b)
	c.checkLimits()
	c.checkPartitionKey()
	for i, op := range b.Operations {
		c.checkOperation(i, op)
	}
	c.checkSharedIDs()
	return c.check
}

// EstimateBytes errs toward more than the service will count, so a batch
// the estimate admits is not refused for its size.
func EstimateBytes(b adapter.Batch) int {
	total := 0
	for _, op := range b.Operations {
		total += operationOverhead + len(op.Body) + len(op.ID) + len(op.IfMatch) + len(op.Condition)
	}
	return total
}

type batchChecker struct {
	batch    adapter.Batch
	keyPaths []string
	check    BatchCheck
}

func (c *batchChecker) problem(format string, args ...any) {
	c.check.Problems = append(c.check.Problems, fmt.Sprintf(format, args...))
}

func (c *batchChecker) warn(format string, args ...any) {
	c.check.Warnings = append(c.check.Warnings, fmt.Sprintf(format, args...))
}

func (c *batchChecker) checkLimits() {
	count, size := len(c.batch.Operations), c.check.Bytes
	switch {
	case count > MaxBatchOperations:
		c.problem("a batch takes at most %d operations; this one has %d", MaxBatchOperations, count)
	case float64(count) >= nearLimit*MaxBatchOperations:
		c.warn("%d operations: close to the service's limit of %d", count, MaxBatchOperations)
	}
	switch {
	case size > MaxBatchBytes:
		c.problem("about %s; the service takes %s", FormatSize(size), FormatSize(MaxBatchBytes))
	case float64(size) >= nearLimit*MaxBatchBytes:
		c.warn("about %s: close to the service's limit of %s", FormatSize(size), FormatSize(MaxBatchBytes))
	}
}

func (c *batchChecker) checkPartitionKey() {
	if len(c.batch.PartitionKey) == len(c.keyPaths) {
		return
	}
	values := "1 value"
	if len(c.keyPaths) != 1 {
		values = fmt.Sprintf("%d values", len(c.keyPaths))
	}
	c.problem("%s is keyed on %s: give %s", strings.Join(c.batch.Scope, "."),
		strings.Join(c.keyPaths, adapter.PartitionKeyPathSeparator), values)
}

func (c *batchChecker) checkOperation(i int, op adapter.Operation) {
	switch op.Kind {
	case adapter.OperationCreate, adapter.OperationUpsert:
		c.checkBody(i, op)
	case adapter.OperationReplace:
		c.checkBody(i, op)
		c.warnUnconditional(op, "replaces")
	case adapter.OperationDelete:
		c.warnUnconditional(op, "deletes")
	case adapter.OperationPatch:
		c.checkPatch(i, op)
		c.warnUnconditional(op, "patches")
	}
}

func (c *batchChecker) warnUnconditional(op adapter.Operation, verb string) {
	if op.IfMatch == "" {
		c.warn("%s %s has no IF MATCH: it %s whatever is there now", strings.ToUpper(string(op.Kind)), op.ID, verb)
	}
}

// checkBody requires an item that names itself and lives in the batch's
// partition.
func (c *batchChecker) checkBody(i int, op adapter.Operation) {
	id, err := bodyID(op.Body)
	if err != nil {
		c.problem("%s: %v", operationLabel(i, op), err)
		return
	}
	if op.Kind == adapter.OperationReplace && id != op.ID {
		c.problem("%s: replaces %q with a body whose id is %q", operationLabel(i, op), op.ID, id)
	}
	if len(c.batch.PartitionKey) != len(c.keyPaths) {
		return
	}
	for k, path := range c.keyPaths {
		values, err := adapter.PartitionKeyValues(op.Body, []string{path})
		switch {
		case err != nil:
			c.problem("%s: body has no %s", operationLabel(i, op), fieldName(path))
		case !sameKey(values[0], c.batch.PartitionKey[k]):
			c.problem("%s: body has %s %s; the batch is for %s", operationLabel(i, op),
				fieldName(path), values[0], c.batch.PartitionKey[k])
		}
	}
}

var (
	errBodyNotObject = errors.New("body is not a JSON object")
	errBodyNoID      = errors.New(`body has no "id"`)
)

func bodyID(body json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return "", errBodyNotObject
	}
	var id string
	if raw, ok := fields["id"]; !ok || json.Unmarshal(raw, &id) != nil || id == "" {
		return "", errBodyNoID
	}
	return id, nil
}

func (c *batchChecker) checkPatch(i int, op adapter.Operation) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(op.Body, &entries); err != nil || len(entries) == 0 {
		c.problem("%s: a patch is a non-empty array of {\"op\", \"path\", \"value\"} entries", operationLabel(i, op))
		return
	}
	for _, fields := range entries {
		if message := c.patchEntryProblem(fields); message != "" {
			c.problem("%s: %s", operationLabel(i, op), message)
		}
	}
}

// patchEntryProblem is what is wrong with one patch entry, empty when
// nothing is. A null value is a value: only a missing one is refused.
func (c *batchChecker) patchEntryProblem(fields map[string]json.RawMessage) string {
	op, path := stringField(fields, "op"), stringField(fields, "path")
	_, hasValue := fields["value"]
	switch {
	case !slices.Contains(patchOps, op):
		return fmt.Sprintf("patch op %q is not one of %s", op, strings.Join(patchOps, ", "))
	case path == "":
		return op + " has no path"
	case path == "/id":
		return "cannot patch /id"
	case slices.Contains(c.keyPaths, path):
		return "cannot patch the partition key " + path
	case op != "remove" && op != "move" && !hasValue:
		return fmt.Sprintf("%s %s has no value", op, path)
	}
	return ""
}

// stringField reads a string member, and is empty for anything else.
func stringField(fields map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(fields[name], &value) // a missing or non-string member reads as empty
	return value
}

// checkSharedIDs refuses two creates of one id, and warns of any other two
// operations on one: legal, and applied in order, but worth a second look.
func (c *batchChecker) checkSharedIDs() {
	first := map[string]int{}
	for i, op := range c.batch.Operations {
		id := ItemID(op)
		if id == "" {
			continue
		}
		earlier, seen := first[id]
		if !seen {
			first[id] = i
			continue
		}
		if op.Kind == adapter.OperationCreate && c.batch.Operations[earlier].Kind == adapter.OperationCreate {
			c.problem("operations %d and %d both create %q", earlier+1, i+1, id)
			continue
		}
		c.warn("operations %d and %d both act on %q; they run in that order", earlier+1, i+1, id)
	}
}

// ItemID is the id of the item op acts on: its own, or its body's for a
// create or an upsert. It is empty when the body names none.
func ItemID(op adapter.Operation) string {
	if op.ID != "" {
		return op.ID
	}
	id, _ := bodyID(op.Body)
	return id
}

// operationLabel names an operation the way a refusal lists it.
func operationLabel(i int, op adapter.Operation) string {
	kind := strings.ToUpper(string(op.Kind))
	if op.ID == "" {
		return fmt.Sprintf("operation %d (%s)", i+1, kind)
	}
	return fmt.Sprintf("operation %d (%s %s)", i+1, kind, jsonString(op.ID))
}

func fieldName(path string) string {
	return strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", ".")
}

// sameKey compares two partition key values by the join-key rule: 1, 1.0
// and 1e0 are equal, and so are -0 and 0, and types are strict.
func sameKey(a, b json.RawMessage) bool {
	return canonicalJSON(a) == canonicalJSON(b)
}

func canonicalJSON(raw json.RawMessage) string {
	rendered, err := canonical.Marshal(raw)
	if err != nil {
		return string(raw)
	}
	return string(rendered)
}

// FormatSize renders a byte count the way the limits are stated.
func FormatSize(n int) string {
	if n < bytesPerMB {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/bytesPerMB)
}

// SamePartition reports whether a and b name one partition, by the rule
// CheckBatch compares keys with.
func SamePartition(a, b adapter.PartitionKey) bool {
	return slices.EqualFunc(a, b, sameKey)
}
