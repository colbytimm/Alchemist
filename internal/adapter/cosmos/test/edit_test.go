package cosmos_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// editAccount stands in for a Cosmos account that answers every item
// request with status, and keeps each one it was sent.
type editAccount struct {
	status     int
	retryAfter string

	mu       sync.Mutex
	requests []capturedEdit
}

type capturedEdit struct {
	method       string
	path         string
	body         []byte
	ifMatch      string
	partitionKey string
}

func (a *editAccount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		_, _ = io.WriteString(w, `{}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.requests = append(a.requests, capturedEdit{
		method: r.Method, path: r.URL.EscapedPath(), body: body,
		ifMatch: r.Header.Get("If-Match"), partitionKey: r.Header.Get("x-ms-documentdb-partitionkey"),
	})
	a.mu.Unlock()
	w.Header().Set("x-ms-request-charge", "10.5")
	w.Header().Set("etag", `"e2"`)
	if a.retryAfter != "" {
		w.Header().Set("x-ms-retry-after-ms", a.retryAfter)
	}
	w.WriteHeader(a.status)
	_, _ = io.WriteString(w, `{"message":"said the service"}`)
}

func (a *editAccount) sent() []capturedEdit {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]capturedEdit(nil), a.requests...)
}

func itemEditor(t *testing.T, handler http.Handler) adapter.ItemEditor {
	t.Helper()
	conn, _ := account(t, handler)
	editor, ok := conn.(adapter.ItemEditor)
	require.True(t, ok, "a cosmos connection edits items")
	return editor
}

var customer = adapter.PartitionKey{json.RawMessage(`"c01"`)}

func patch(body string, condition string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(body), Condition: condition}
}

// sentPatch decodes the one patch request sent.
func sentPatch(t *testing.T, acct *editAccount) (condition string, operations []json.RawMessage) {
	t.Helper()
	requests := acct.sent()
	require.Len(t, requests, 1)
	var sent struct {
		Condition  string            `json:"condition"`
		Operations []json.RawMessage `json:"operations"`
	}
	require.NoError(t, json.Unmarshal(requests[0].body, &sent), "the request body is valid JSON: %s", requests[0].body)
	return sent.Condition, sent.Operations
}

func TestAPatchValueIsSentAsWritten(t *testing.T) {
	for _, value := range []string{`null`, `false`, `0`, `""`, `12345678901234567890`, `{"region":"west","n":[1,2]}`} {
		t.Run(value, func(t *testing.T) {
			acct := &editAccount{status: http.StatusOK}

			_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
				patch(`[{"op":"set","path":"/x","value":`+value+`}]`, ""))

			require.NoError(t, err)
			_, operations := sentPatch(t, acct)
			require.Len(t, operations, 1)
			assert.JSONEq(t, `{"op":"set","path":"/x","value":`+value+`}`, string(operations[0]))
			assert.Contains(t, string(operations[0]), value, "the value's own digits")
		})
	}
}

func TestAConditionIsSentAsWritten(t *testing.T) {
	acct := &editAccount{status: http.StatusOK}
	condition := `FROM o WHERE (o.note = "say \"hi\"" AND o.path = "a\\b")`

	_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
		patch(`[{"op":"remove","path":"/tmp"}]`, condition))

	require.NoError(t, err)
	sent, _ := sentPatch(t, acct)
	assert.Equal(t, condition, sent)
}

func TestAnEditReportsWhatTheServiceAnswered(t *testing.T) {
	acct := &editAccount{status: http.StatusOK}

	result, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
		patch(`[{"op":"set","path":"/x","value":1}]`, ""))

	require.NoError(t, err)
	assert.Equal(t, adapter.OperationResult{Outcome: adapter.OperationApplied, Status: "200 OK", ETag: `"e2"`, RequestCharge: 10.5}, result)
	requests := acct.sent()
	assert.Equal(t, http.MethodPatch, requests[0].method)
}

func TestADeleteIsSentOnItsVersion(t *testing.T) {
	acct := &editAccount{status: http.StatusNoContent}

	_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
		adapter.Operation{Kind: adapter.OperationDelete, ID: `a"b/c`, IfMatch: `"e1"`})

	require.NoError(t, err)
	requests := acct.sent()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodDelete, requests[0].method)
	assert.Equal(t, `"e1"`, requests[0].ifMatch)
	assert.Contains(t, requests[0].path, "a%22b%2Fc", "the id is escaped into the path")
}

func TestAnEditIsClassifiedByItsAnswer(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
		want       error
		unknown    bool
	}{
		{name: "a failed condition", status: http.StatusPreconditionFailed, want: adapter.ErrPreconditionFailed},
		{name: "a missing item", status: http.StatusNotFound, want: adapter.ErrItemNotFound},
		{name: "a request timeout", status: http.StatusRequestTimeout, want: adapter.ErrWriteOutcomeUnknown, unknown: true},
		{name: "a service error", status: http.StatusServiceUnavailable, want: adapter.ErrWriteOutcomeUnknown, unknown: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := &editAccount{status: tt.status, retryAfter: tt.retryAfter}

			result, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
				patch(`[{"op":"set","path":"/x","value":1}]`, "FROM o WHERE (true)"))

			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, tt.unknown, errors.Is(err, adapter.ErrWriteOutcomeUnknown))
			assert.Equal(t, 10.5, result.RequestCharge, "the charge of an answered refusal")
			assert.Contains(t, err.Error(), "said the service")
			assert.Len(t, acct.sent(), 1, "sent once, never replayed")
		})
	}
}

func TestAThrottledEditNamesItsWait(t *testing.T) {
	acct := &editAccount{status: http.StatusTooManyRequests, retryAfter: "250"}

	_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer,
		patch(`[{"op":"set","path":"/x","value":1}]`, ""))

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, err, &throttled)
	assert.Equal(t, 250*time.Millisecond, throttled.RetryAfter)
	assert.NotErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
	assert.Len(t, acct.sent(), 1, "the pool, not azcore, waits it out")
}

func TestAnEditToADeadEndpointWasNotSent(t *testing.T) {
	conn, _ := connectTo(t, "https://127.0.0.1:1")
	editor, ok := conn.(adapter.ItemEditor)
	require.True(t, ok)

	_, err := editor.EditItem(context.Background(), []string{"sales", "orders"}, customer, patch(`[{"op":"set","path":"/x","value":1}]`, ""))

	require.Error(t, err)
	assert.NotErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
}

func TestAnEditThatCannotBeSentIsRefusedUnsent(t *testing.T) {
	tests := []struct {
		name string
		key  adapter.PartitionKey
		op   adapter.Operation
		want error
	}{
		{name: "a replace", key: customer, op: adapter.Operation{Kind: adapter.OperationReplace, ID: "o1", Body: json.RawMessage(`{}`)}, want: adapter.ErrUnsupported},
		{name: "an integer key past 2^53", key: adapter.PartitionKey{json.RawMessage(`9007199254740993`)}, op: patch(`[{"op":"set","path":"/x","value":1}]`, ""), want: adapter.ErrNoPartitionKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := &editAccount{status: http.StatusOK}

			_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, tt.key, tt.op)

			require.ErrorIs(t, err, tt.want)
			assert.Empty(t, acct.sent())
		})
	}
}

func TestScanQuery(t *testing.T) {
	filter := adapter.ScanFilter{Alias: "o", Predicate: `o.status = "shipped"`}
	since := time.Unix(1_800_000_000, 0)
	tests := []struct {
		name     string
		request  adapter.ScanRequest
		keyPaths []string
		want     string
	}{
		{name: "every item", want: "SELECT * FROM c"},
		{name: "since", request: adapter.ScanRequest{Since: since}, want: "SELECT * FROM c WHERE c._ts >= @since"},
		{name: "filtered", request: adapter.ScanRequest{Filter: filter}, want: `SELECT * FROM o WHERE (o.status = "shipped")`},
		{
			name: "filtered since", request: adapter.ScanRequest{Filter: filter, Since: since},
			want: `SELECT * FROM o WHERE o._ts >= @since AND (o.status = "shipped")`,
		},
		{
			name: "filtered identities", request: adapter.ScanRequest{Filter: filter, Projection: adapter.ScanIdentity}, keyPaths: []string{"/customerId"},
			want: `SELECT VALUE {"id": o["id"], "_rid": o["_rid"], "_self": o["_self"], "_etag": o["_etag"], "_attachments": o["_attachments"], "_ts": o["_ts"], "customerId": o["customerId"]} FROM o WHERE (o.status = "shipped")`,
		},
		{
			name: "filtered identities under a nested key", request: adapter.ScanRequest{Filter: filter, Projection: adapter.ScanIdentity}, keyPaths: []string{"/shipTo/region"},
			want: `SELECT VALUE {"id": o["id"], "_rid": o["_rid"], "_self": o["_self"], "_etag": o["_etag"], "_attachments": o["_attachments"], "_ts": o["_ts"], "shipTo": {"region": o["shipTo"]["region"]}} FROM o WHERE (o.status = "shipped")`,
		},
		{
			name: "filtered identities under two key paths", request: adapter.ScanRequest{Filter: filter, Projection: adapter.ScanIdentity, Since: since}, keyPaths: []string{"/tenantId", "/userId"},
			want: `SELECT VALUE {"id": o["id"], "_rid": o["_rid"], "_self": o["_self"], "_etag": o["_etag"], "_attachments": o["_attachments"], "_ts": o["_ts"], "tenantId": o["tenantId"], "userId": o["userId"]} FROM o WHERE o._ts >= @since AND (o.status = "shipped")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cosmos.ScanQuery(tt.request, tt.keyPaths))
		})
	}
}

func deleteOn(version string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationDelete, ID: "o1", IfMatch: version}
}

func TestADeleteCarriesItsVersionAndKeyVerbatim(t *testing.T) {
	tests := []struct {
		name string
		key  adapter.PartitionKey
		want string
	}{
		{name: "a plain key", key: customer, want: `["c01"]`},
		{name: "a two-path key", key: adapter.PartitionKey{json.RawMessage(`"t1"`), json.RawMessage(`"u1"`)}, want: `["t1","u1"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := &editAccount{status: http.StatusNoContent}

			result, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, tt.key, deleteOn(`"00000a00-0000-0000-0000-65f1a2b30000"`))

			require.NoError(t, err)
			assert.Equal(t, "204 No Content", result.Status)
			requests := acct.sent()
			require.Len(t, requests, 1)
			assert.Equal(t, http.MethodDelete, requests[0].method)
			assert.Equal(t, `"00000a00-0000-0000-0000-65f1a2b30000"`, requests[0].ifMatch, "the version verbatim, quotes included")
			assert.JSONEq(t, tt.want, requests[0].partitionKey)
			assert.Empty(t, requests[0].body)
		})
	}
}

func TestADeleteIsClassifiedByItsAnswer(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "a changed item", status: http.StatusPreconditionFailed, want: adapter.ErrPreconditionFailed},
		{name: "a missing item", status: http.StatusNotFound, want: adapter.ErrItemNotFound},
		{name: "a service error", status: http.StatusServiceUnavailable, want: adapter.ErrWriteOutcomeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := &editAccount{status: tt.status}

			_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer, deleteOn(`"e1"`))

			require.ErrorIs(t, err, tt.want)
			assert.Len(t, acct.sent(), 1, "sent once, never replayed")
		})
	}
}

func TestAThrottledDeleteIsLeftToThePool(t *testing.T) {
	acct := &editAccount{status: http.StatusTooManyRequests, retryAfter: "100"}

	_, err := itemEditor(t, acct).EditItem(context.Background(), []string{"sales", "orders"}, customer, deleteOn(`"e1"`))

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, err, &throttled)
	assert.Equal(t, 100*time.Millisecond, throttled.RetryAfter)
	assert.Len(t, acct.sent(), 1)
}
