package cosmos_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// batchAccount stands in for a Cosmos account that answers every batch
// with status and body, and keeps each batch request it was sent.
type batchAccount struct {
	status int
	body   string

	mu       sync.Mutex
	requests []capturedBatch
}

type capturedBatch struct {
	body         []byte
	partitionKey string
}

func (a *batchAccount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		_, _ = io.WriteString(w, `{}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.requests = append(a.requests, capturedBatch{body: body, partitionKey: r.Header.Get("x-ms-documentdb-partitionkey")})
	a.mu.Unlock()
	w.Header().Set("x-ms-request-charge", "12.5")
	w.WriteHeader(a.status)
	_, _ = io.WriteString(w, a.body)
}

func (a *batchAccount) sent() []capturedBatch {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]capturedBatch(nil), a.requests...)
}

func committing() *batchAccount {
	return &batchAccount{status: http.StatusOK, body: `[{"statusCode":201,"requestCharge":12.5,"eTag":"\"e1\""}]`}
}

func batcher(t *testing.T, handler http.Handler) adapter.Batcher {
	t.Helper()
	conn, _ := account(t, handler)
	b, ok := conn.(adapter.Batcher)
	require.True(t, ok, "a cosmos connection runs batches")
	return b
}

func ordersBatch(ops ...adapter.Operation) adapter.Batch {
	return adapter.Batch{
		Scope:        []string{"sales", "orders"},
		PartitionKey: adapter.PartitionKey{json.RawMessage(`"c01"`)},
		Operations:   ops,
	}
}

// sentOperations decodes the operations of the one batch request sent.
func sentOperations(t *testing.T, acct *batchAccount) []map[string]json.RawMessage {
	t.Helper()
	requests := acct.sent()
	require.Len(t, requests, 1)
	var ops []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(requests[0].body, &ops), "the request body is valid JSON: %s", requests[0].body)
	return ops
}

func stringField(t *testing.T, fields map[string]json.RawMessage, name string) string {
	t.Helper()
	var value string
	require.NoError(t, json.Unmarshal(fields[name], &value))
	return value
}

func TestAnIDIsSentAsWritten(t *testing.T) {
	for _, id := range []string{`a"b`, `a\b`, "plain"} {
		t.Run(id, func(t *testing.T) {
			acct := &batchAccount{status: http.StatusOK, body: `[{"statusCode":204},{"statusCode":200}]`}

			_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(
				adapter.Operation{Kind: adapter.OperationDelete, ID: id},
				adapter.Operation{Kind: adapter.OperationRead, ID: id},
			))

			require.NoError(t, err)
			ops := sentOperations(t, acct)
			assert.Equal(t, id, stringField(t, ops[0], "id"))
			assert.Equal(t, id, stringField(t, ops[1], "id"))
		})
	}
}

func TestAPatchIsSentEntryForEntry(t *testing.T) {
	acct := committing()
	patch := `[{"op":"set","path":"/x","value":null},{"op":"add","path":"/big","value":1e400},` +
		`{"op":"replace","path":"/long","value":12345678901234567890},{"op":"remove","path":"/gone"},{"op":"incr","path":"/n","value":5}]`

	_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(adapter.Operation{
		Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(patch),
		Condition: `FROM c WHERE c.note = "a\b"`, IfMatch: `"e0"`,
	}))

	require.NoError(t, err)
	ops := sentOperations(t, acct)
	assert.Equal(t, `"e0"`, stringField(t, ops[0], "ifMatch"))
	var sent struct {
		Condition  string            `json:"condition"`
		Operations []json.RawMessage `json:"operations"`
	}
	require.NoError(t, json.Unmarshal(ops[0]["resourceBody"], &sent))
	assert.Equal(t, `FROM c WHERE c.note = "a\b"`, sent.Condition)
	require.Len(t, sent.Operations, 5)
	assert.JSONEq(t, `{"op":"set","path":"/x","value":null}`, string(sent.Operations[0]), "a null value is sent, not dropped")
	assert.Contains(t, string(sent.Operations[1]), `"value":1e400`)
	assert.Contains(t, string(sent.Operations[2]), `"value":12345678901234567890`)
	assert.JSONEq(t, `{"op":"remove","path":"/gone"}`, string(sent.Operations[3]))
	assert.JSONEq(t, `{"op":"incr","path":"/n","value":5}`, string(sent.Operations[4]))
}

func TestAPatchTheSDKCannotSendIsRefusedBeforeAnything(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  string
	}{
		{name: "move", patch: `[{"op":"move","from":"/a","path":"/b"}]`, want: "move has no call in the Go SDK"},
		{name: "a fractional incr", patch: `[{"op":"incr","path":"/n","value":1.5}]`, want: "only whole numbers"},
		{name: "an incr past 64 bits", patch: `[{"op":"incr","path":"/n","value":12345678901234567890}]`, want: "only whole numbers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := committing()

			_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(
				adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(tt.patch)}))

			require.ErrorContains(t, err, tt.want)
			assert.NotErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
			assert.Empty(t, acct.sent())
		})
	}
}

func TestPartitionKeyShapes(t *testing.T) {
	tests := []struct {
		name string
		key  []string
		want azcosmos.PartitionKey
	}{
		{name: "a string", key: []string{`"c01"`}, want: azcosmos.NewPartitionKeyString("c01")},
		{name: "a number", key: []string{`42.5`}, want: azcosmos.NewPartitionKeyNumber(42.5)},
		{name: "a boolean", key: []string{`true`}, want: azcosmos.NewPartitionKeyBool(true)},
		{name: "null", key: []string{`null`}, want: azcosmos.NewPartitionKey().AppendNull()},
		{name: "a hierarchical key", key: []string{`"t"`, `7`, `false`},
			want: azcosmos.NewPartitionKeyString("t").AppendNumber(7).AppendBool(false)},
		{name: "a leading null", key: []string{`null`, `"eu"`}, want: azcosmos.NewPartitionKey().AppendNull().AppendString("eu")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var key adapter.PartitionKey
			for _, value := range tt.key {
				key = append(key, json.RawMessage(value))
			}

			got, err := cosmos.PartitionKey(key)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAPartitionKeyOfNoScalarIsRefused(t *testing.T) {
	_, err := cosmos.PartitionKey(adapter.PartitionKey{json.RawMessage(`{"a":1}`)})

	require.Error(t, err)
}

func TestTheBatchIsSentUnderItsPartitionKey(t *testing.T) {
	acct := committing()

	_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(
		adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"o1","customerId":"c01"}`)}))

	require.NoError(t, err)
	assert.Equal(t, `["c01"]`, acct.sent()[0].partitionKey)
}

func TestARollbackIsAnAnswer(t *testing.T) {
	acct := &batchAccount{status: http.StatusMultiStatus, body: `[{"statusCode":424,"requestCharge":0},` +
		`{"statusCode":404,"requestCharge":1.25},{"statusCode":424}]`}

	result, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(
		adapter.Operation{Kind: adapter.OperationDelete, ID: "a"},
		adapter.Operation{Kind: adapter.OperationDelete, ID: "b"},
		adapter.Operation{Kind: adapter.OperationDelete, ID: "c"},
	))

	require.NoError(t, err)
	assert.False(t, result.Committed)
	require.Len(t, result.Results, 3)
	assert.Equal(t, adapter.OperationSkipped, result.Results[0].Outcome)
	assert.Equal(t, "424 Failed Dependency", result.Results[0].Status)
	assert.Equal(t, adapter.OperationFailed, result.Results[1].Outcome)
	assert.Equal(t, "404 Not Found", result.Results[1].Status)
	assert.InDelta(t, 1.25, result.Results[1].RequestCharge, 0.001)
	assert.InDelta(t, 12.5, result.Stats.RequestCharge, 0.001)
}

func TestACommitReportsEachResult(t *testing.T) {
	result, err := batcher(t, committing()).ExecuteBatch(context.Background(), ordersBatch(
		adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"o1","customerId":"c01"}`)}))

	require.NoError(t, err)
	assert.True(t, result.Committed)
	assert.Equal(t, adapter.OperationResult{Outcome: adapter.OperationApplied, Status: "201 Created", ETag: `"e1"`, RequestCharge: 12.5},
		result.Results[0])
}

func TestAnUnansweredBatchHasAnUnknownOutcome(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			acct := &batchAccount{status: status, body: `{"message":"try later"}`}

			_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(adapter.Operation{Kind: adapter.OperationDelete, ID: "a"}))

			require.ErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
			assert.Len(t, acct.sent(), 1, "sent once, never replayed")
		})
	}
}

func TestARefusedBatchWasNotApplied(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			acct := &batchAccount{status: status, body: `{"message":"no"}`}

			_, err := batcher(t, acct).ExecuteBatch(context.Background(), ordersBatch(adapter.Operation{Kind: adapter.OperationDelete, ID: "a"}))

			require.Error(t, err)
			assert.NotErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
			assert.Equal(t, fmt.Sprintf("cosmos: batch sales.orders: %d %s: no", status, http.StatusText(status)), err.Error(),
				"the service's own words")
			assert.Len(t, acct.sent(), 1, "sent once, never replayed")
		})
	}
}

func TestAConnectionDroppedMidAnswerHasAnUnknownOutcome(t *testing.T) {
	var posts int
	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		mu.Lock()
		posts++
		mu.Unlock()
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `[{"statusCode":`)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hijacker.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	})

	_, err := batcher(t, handler).ExecuteBatch(context.Background(), ordersBatch(adapter.Operation{Kind: adapter.OperationDelete, ID: "a"}))

	require.ErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, posts)
}

func TestABatchPastItsDeadlineHasAnUnknownOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), stallFor/10)
	t.Cleanup(cancel)

	_, err := batcher(t, stalling()).ExecuteBatch(ctx, ordersBatch(adapter.Operation{Kind: adapter.OperationDelete, ID: "a"}))

	require.ErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, "cosmos: batch sales.orders: timed out", err.Error())
}

func TestWriteError(t *testing.T) {
	dial := &url.Error{Op: "Post", URL: "https://acct.example/dbs", Err: &net.OpError{Op: "dial", Net: "tcp", Err: io.EOF}}
	dns := &url.Error{Op: "Post", URL: "https://acct.example/dbs", Err: &net.DNSError{Err: "no such host", Name: "acct.example"}}
	reset := &url.Error{Op: "Post", URL: "https://acct.example/dbs", Err: &net.OpError{Op: "read", Net: "tcp", Err: io.ErrUnexpectedEOF}}
	tests := []struct {
		name    string
		err     error
		unknown bool
	}{
		{name: "a dial failure", err: dial},
		{name: "a name that did not resolve", err: dns},
		{name: "a refusal", err: &azcore.ResponseError{StatusCode: http.StatusBadRequest}},
		{name: "a throttle", err: &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}},
		{name: "a request timeout", err: &azcore.ResponseError{StatusCode: http.StatusRequestTimeout}, unknown: true},
		{name: "a server error", err: &azcore.ResponseError{StatusCode: http.StatusBadGateway}, unknown: true},
		{name: "a connection reset after sending", err: reset, unknown: true},
		{name: "a passed deadline", err: context.DeadlineExceeded, unknown: true},
		{name: "a cancellation", err: context.Canceled, unknown: true},
		{name: "anything else", err: io.ErrClosedPipe, unknown: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cosmos.WriteError("batch sales.orders", tt.err)

			assert.Equal(t, tt.unknown, errors.Is(err, adapter.ErrWriteOutcomeUnknown), err)
			assert.NotContains(t, err.Error(), "acct.example")
		})
	}
}

func TestDraftReplaceDropsTheSystemFields(t *testing.T) {
	conn, _ := connectTo(t, "https://localhost:8081")
	drafter, ok := conn.(adapter.ItemDrafter)
	require.True(t, ok)

	op, err := drafter.DraftReplace(json.RawMessage(`{"id":"o1","status":"open","_rid":"r","_self":"s","_etag":"\"0800\"","_attachments":"a","_ts":1,"_lsn":2}`))

	require.NoError(t, err)
	assert.Equal(t, adapter.Operation{
		Kind: adapter.OperationReplace, ID: "o1", Body: json.RawMessage(`{"id":"o1","status":"open"}`), IfMatch: `"0800"`,
	}, op)
}

func TestDraftReplaceRefusesAnItemWithNoID(t *testing.T) {
	conn, _ := connectTo(t, "https://localhost:8081")
	drafter, ok := conn.(adapter.ItemDrafter)
	require.True(t, ok)

	_, err := drafter.DraftReplace(json.RawMessage(`{"status":"open"}`))

	require.Error(t, err)
}
