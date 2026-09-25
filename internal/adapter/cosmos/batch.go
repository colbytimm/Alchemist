package cosmos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var (
	_ adapter.Batcher     = (*connection)(nil)
	_ adapter.ItemDrafter = (*connection)(nil)
)

// systemFields are the properties the service writes on every item, which a
// replace must not send back.
var systemFields = []string{"_rid", "_self", "_etag", "_attachments", "_ts", "_lsn"}

var errNoID = errors.New("the item has no id")

// ExecuteBatch sends b as one transactional batch, once. A rolled-back
// batch is an answer, not an error.
func (c *connection) ExecuteBatch(ctx context.Context, b adapter.Batch) (adapter.BatchResult, error) {
	op := "batch " + pathText(b.Scope)
	container, err := c.containerAt(op, b.Scope)
	if err != nil {
		return adapter.BatchResult{}, err
	}
	key, err := PartitionKey(b.PartitionKey)
	if err != nil {
		return adapter.BatchResult{}, err
	}
	batch := container.NewTransactionalBatch(key)
	for i, operation := range b.Operations {
		if err := addOperation(&batch, operation); err != nil {
			return adapter.BatchResult{}, fmt.Errorf("cosmos: %s: operation %d: %w", op, i+1, err)
		}
	}
	start := time.Now()
	resp, err := container.ExecuteTransactionalBatch(withoutRetries(ctx), batch, nil)
	if err != nil {
		return adapter.BatchResult{}, WriteError(op, err)
	}
	return batchResult(resp, time.Since(start)), nil
}

// addOperation adds op to batch. The SDK writes an id into the request with
// %s, unescaped, so it is handed over JSON-escaped.
func addOperation(batch *azcosmos.TransactionalBatch, op adapter.Operation) error {
	options := itemOptions(op.IfMatch)
	id := escaped(op.ID)
	switch op.Kind {
	case adapter.OperationCreate:
		batch.CreateItem(op.Body, nil)
	case adapter.OperationUpsert:
		batch.UpsertItem(op.Body, options)
	case adapter.OperationReplace:
		batch.ReplaceItem(id, op.Body, options)
	case adapter.OperationDelete:
		batch.DeleteItem(id, options)
	case adapter.OperationRead:
		batch.ReadItem(id, nil)
	case adapter.OperationPatch:
		patch, err := patchOperations(op.Body, op.Condition)
		if err != nil {
			return err
		}
		batch.PatchItem(id, patch, options)
	default:
		return fmt.Errorf("unknown operation %q", op.Kind)
	}
	return nil
}

func itemOptions(ifMatch string) *azcosmos.TransactionalBatchItemOptions {
	if ifMatch == "" {
		return nil
	}
	etag := azcore.ETag(ifMatch)
	return &azcosmos.TransactionalBatchItemOptions{IfMatchETag: &etag}
}

// patchOperations maps each patch entry to its SDK call. Values stay raw
// JSON: decoded, a null would become a nil the SDK leaves out of the
// request, and a number could lose digits.
func patchOperations(body json.RawMessage, condition string) (azcosmos.PatchOperations, error) {
	var patch azcosmos.PatchOperations
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return patch, fmt.Errorf("patch: %w", err)
	}
	for _, entry := range entries {
		if err := appendPatch(&patch, entry); err != nil {
			return patch, err
		}
	}
	if condition != "" {
		patch.SetCondition(escaped(condition))
	}
	return patch, nil
}

func appendPatch(patch *azcosmos.PatchOperations, entry map[string]json.RawMessage) error {
	var op, path string
	if json.Unmarshal(entry["op"], &op) != nil || json.Unmarshal(entry["path"], &path) != nil {
		return errors.New("patch: every entry needs a string op and path")
	}
	value, hasValue := entry["value"]
	switch {
	case op == "remove":
		patch.AppendRemove(path)
		return nil
	case op == "move":
		return errors.New("patch: move has no call in the Go SDK, so it cannot be sent")
	case !hasValue:
		return fmt.Errorf("patch: %s %s has no value", op, path)
	}
	switch op {
	case "add":
		patch.AppendAdd(path, value)
	case "set":
		patch.AppendSet(path, value)
	case "replace":
		patch.AppendReplace(path, value)
	case "incr":
		step, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil {
			return fmt.Errorf("patch: incr %s by %s: the Go SDK sends only whole numbers that fit 64 bits", path, value)
		}
		patch.AppendIncrement(path, step)
	default:
		return fmt.Errorf("patch: unknown op %q", op)
	}
	return nil
}

// escaped is s as the inside of a JSON string.
func escaped(s string) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s) // a string always encodes
	quoted := bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	return string(quoted[1 : len(quoted)-1])
}

func batchResult(resp azcosmos.TransactionalBatchResponse, elapsed time.Duration) adapter.BatchResult {
	result := adapter.BatchResult{
		Committed: resp.Success,
		Stats: adapter.Stats{
			RequestCharge: float64(resp.RequestCharge),
			Elapsed:       elapsed,
			RowCount:      len(resp.OperationResults),
		},
	}
	for _, r := range resp.OperationResults {
		result.Results = append(result.Results, adapter.OperationResult{
			Outcome:       outcome(int(r.StatusCode)),
			Status:        statusLine(int(r.StatusCode)),
			ETag:          string(r.ETag),
			Body:          r.ResourceBody,
			RequestCharge: float64(r.RequestCharge),
		})
	}
	return result
}

func outcome(status int) adapter.OperationOutcome {
	switch {
	case status == http.StatusFailedDependency:
		return adapter.OperationSkipped
	case status >= http.StatusBadRequest:
		return adapter.OperationFailed
	}
	return adapter.OperationApplied
}

// DraftReplace sends item back as it was read, less the fields the service
// writes, on condition that it has not changed since.
func (c *connection) DraftReplace(item json.RawMessage) (adapter.Operation, error) {
	var head struct {
		ID   string `json:"id"`
		ETag string `json:"_etag"`
	}
	if err := json.Unmarshal(item, &head); err != nil || head.ID == "" {
		return adapter.Operation{}, fmt.Errorf("cosmos: draft replace: %w", errNoID)
	}
	body, err := adapter.WithoutFields(item, systemFields...)
	if err != nil {
		return adapter.Operation{}, fmt.Errorf("cosmos: draft replace: %w", err)
	}
	return adapter.Operation{Kind: adapter.OperationReplace, ID: head.ID, Body: body, IfMatch: head.ETag}, nil
}
