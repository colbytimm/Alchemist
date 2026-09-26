package mock_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

var ordersPath = []string{"sales", "orders"}

func order(id, customer string) json.RawMessage {
	return json.RawMessage(`{"id":"` + id + `","customerId":"` + customer + `","status":"open"}`)
}

func batcher(t *testing.T, a *mock.Adapter) adapter.Batcher {
	t.Helper()
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	b, ok := conn.(adapter.Batcher)
	require.True(t, ok, "a mock connection runs batches")
	return b
}

func forCustomer(customer string, ops ...adapter.Operation) adapter.Batch {
	return adapter.Batch{
		Scope:        ordersPath,
		PartitionKey: adapter.PartitionKey{json.RawMessage(`"` + customer + `"`)},
		Operations:   ops,
	}
}

func create(body json.RawMessage) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationCreate, Body: body}
}

func ids(t *testing.T, items []json.RawMessage) []string {
	t.Helper()
	var listed []string
	for _, item := range items {
		var head struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(item, &head))
		listed = append(listed, head.ID)
	}
	return listed
}

func etagOf(t *testing.T, item json.RawMessage) string {
	t.Helper()
	var head struct {
		ETag string `json:"_etag"`
	}
	require.NoError(t, json.Unmarshal(item, &head))
	return head.ETag
}

func TestACommittedBatchAppliesEveryOperation(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01"), order("o2", "c01")))

	result, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		create(order("o3", "c01")),
		adapter.Operation{Kind: adapter.OperationDelete, ID: "o1"},
		adapter.Operation{Kind: adapter.OperationPatch, ID: "o2", Body: json.RawMessage(`[{"op":"set","path":"/status","value":"shipped"}]`)},
		adapter.Operation{Kind: adapter.OperationRead, ID: "o2"},
	))

	require.NoError(t, err)
	assert.True(t, result.Committed)
	require.Len(t, result.Results, 4)
	for _, r := range result.Results {
		assert.Equal(t, adapter.OperationApplied, r.Outcome)
	}
	assert.Equal(t, []string{"201 Created", "204 No Content", "200 OK", "200 OK"},
		[]string{result.Results[0].Status, result.Results[1].Status, result.Results[2].Status, result.Results[3].Status})
	body, meta, err := adapter.SplitSystemFields(result.Results[3].Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"o2","customerId":"c01","status":"shipped"}`, string(body), "the read sees the patch before it")
	assert.Equal(t, result.Results[3].ETag, meta.Version)
	assert.Equal(t, []string{"o2", "o3"}, ids(t, a.Items(ordersPath)))
	assert.InDelta(t, 7+7+10+1, result.Stats.RequestCharge, 0.001)
	assert.Equal(t, 4, result.Stats.RowCount)
}

func TestAFailureRollsBackEveryOperation(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")), mock.WithBatchFailure(2, http.StatusTooManyRequests))
	before := a.Items(ordersPath)

	result, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		create(order("o2", "c01")),
		create(order("o3", "c01")),
		adapter.Operation{Kind: adapter.OperationDelete, ID: "o1"},
	))

	require.NoError(t, err, "a rollback is an answer")
	assert.False(t, result.Committed)
	assert.Equal(t, []adapter.OperationOutcome{adapter.OperationSkipped, adapter.OperationFailed, adapter.OperationSkipped},
		outcomes(result))
	assert.Equal(t, "429 Too Many Requests", result.Results[1].Status)
	assert.Equal(t, "424 Failed Dependency", result.Results[0].Status)
	assert.Equal(t, before, a.Items(ordersPath), "nothing was written")
}

func outcomes(result adapter.BatchResult) []adapter.OperationOutcome {
	var listed []adapter.OperationOutcome
	for _, r := range result.Results {
		listed = append(listed, r.Outcome)
	}
	return listed
}

func TestTheStoreDecidesWhichOperationFails(t *testing.T) {
	seeded := func() *mock.Adapter { return mock.New(mock.WithItems(ordersPath, order("o1", "c01"))) }
	tests := []struct {
		name string
		op   adapter.Operation
		want string
	}{
		{name: "a create of an id that exists", op: create(order("o1", "c01")), want: "409 Conflict"},
		{name: "a replace of a missing id", op: adapter.Operation{Kind: adapter.OperationReplace, ID: "o9", Body: order("o9", "c01")}, want: "404 Not Found"},
		{name: "a delete of a missing id", op: adapter.Operation{Kind: adapter.OperationDelete, ID: "o9"}, want: "404 Not Found"},
		{name: "a read of a missing id", op: adapter.Operation{Kind: adapter.OperationRead, ID: "o9"}, want: "404 Not Found"},
		{name: "a patch of a missing id", op: adapter.Operation{Kind: adapter.OperationPatch, ID: "o9", Body: json.RawMessage(`[]`)}, want: "404 Not Found"},
		{name: "a stale version", op: adapter.Operation{Kind: adapter.OperationDelete, ID: "o1", IfMatch: `"stale"`}, want: "412 Precondition Failed"},
		{name: "an item of another partition", op: create(order("o5", "c02")), want: "400 Bad Request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := batcher(t, seeded()).ExecuteBatch(context.Background(), forCustomer("c01", tt.op))

			require.NoError(t, err)
			assert.False(t, result.Committed)
			assert.Equal(t, adapter.OperationFailed, result.Results[0].Outcome)
			assert.Equal(t, tt.want, result.Results[0].Status)
		})
	}
}

func TestAWriteGivesTheItemANewVersion(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))
	first := etagOf(t, a.Items(ordersPath)[0])

	result, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		adapter.Operation{Kind: adapter.OperationReplace, ID: "o1", Body: order("o1", "c01"), IfMatch: first}))

	require.NoError(t, err)
	require.True(t, result.Committed)
	second := etagOf(t, a.Items(ordersPath)[0])
	assert.NotEqual(t, first, second)
	assert.Equal(t, second, result.Results[0].ETag)
}

func TestAWriteBodyComesBackOnlyWhenTheBatchReads(t *testing.T) {
	a := mock.New()

	result, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01", create(order("o1", "c01"))))

	require.NoError(t, err)
	assert.Empty(t, result.Results[0].Body)
}

func TestAdaptersShareNoItems(t *testing.T) {
	first, second := mock.New(), mock.New()

	_, err := batcher(t, first).ExecuteBatch(context.Background(), forCustomer("c01", create(order("o1", "c01"))))

	require.NoError(t, err)
	assert.Len(t, first.Items(ordersPath), 1)
	assert.Empty(t, second.Items(ordersPath))
}

func TestInjectedBatchFailures(t *testing.T) {
	t.Run("not applied", func(t *testing.T) {
		a := mock.New(mock.WithError(mock.OpBatch))

		_, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01", create(order("o1", "c01"))))

		require.Error(t, err)
		assert.NotErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
		assert.Empty(t, a.Items(ordersPath))
	})
	t.Run("outcome unknown", func(t *testing.T) {
		a := mock.New(mock.WithError(mock.OpBatchUnknown))

		_, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01", create(order("o1", "c01"))))

		require.ErrorIs(t, err, adapter.ErrWriteOutcomeUnknown)
		assert.Len(t, a.Items(ordersPath), 1, "the batch was applied before its answer was lost")
	})
}

func TestPatchOperations(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  string
	}{
		{name: "add", patch: `[{"op":"add","path":"/note","value":"x"}]`, want: `{"id":"o1","customerId":"c01","total":2,"note":"x"}`},
		{name: "set", patch: `[{"op":"set","path":"/total","value":5}]`, want: `{"id":"o1","customerId":"c01","total":5}`},
		{name: "replace", patch: `[{"op":"replace","path":"/total","value":null}]`, want: `{"id":"o1","customerId":"c01","total":null}`},
		{name: "remove", patch: `[{"op":"remove","path":"/total"}]`, want: `{"id":"o1","customerId":"c01"}`},
		{name: "incr", patch: `[{"op":"incr","path":"/total","value":3}]`, want: `{"id":"o1","customerId":"c01","total":5}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := mock.New(mock.WithItems(ordersPath, json.RawMessage(`{"id":"o1","customerId":"c01","total":2}`)))

			result, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
				adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(tt.patch)}))

			require.NoError(t, err)
			require.True(t, result.Committed)
			item, _, err := adapter.SplitSystemFields(a.Items(ordersPath)[0])
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(item))
		})
	}
}

func TestANestedPatchNeedsItsParent(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, json.RawMessage(`{"id":"o1","customerId":"c01","ship":{"city":"x"},"lines":[1,2]}`)))

	missing, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(`[{"op":"set","path":"/a/b","value":1}]`)}))
	require.NoError(t, err)
	present, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(
			`[{"op":"set","path":"/ship/region","value":"w"},{"op":"set","path":"/lines/1","value":9}]`)}))
	require.NoError(t, err)

	assert.False(t, missing.Committed, "set creates the last step of a path, never a parent")
	past, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(`[{"op":"replace","path":"/lines/5","value":0}]`)}))
	require.NoError(t, err)
	assert.False(t, past.Committed, "replace needs the element")
	assert.True(t, present.Committed)
	assert.JSONEq(t, `{"id":"o1","customerId":"c01","ship":{"city":"x","region":"w"},"lines":[1,9]}`,
		string(withoutSystemFields(t, a.Items(ordersPath)[0])))
	appended, err := batcher(t, a).ExecuteBatch(context.Background(), forCustomer("c01",
		adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(`[{"op":"set","path":"/lines/7","value":3}]`)}))
	require.NoError(t, err)
	require.True(t, appended.Committed)
	assert.Contains(t, string(a.Items(ordersPath)[0]), `"lines":[1,9,3]`, "past the end a set appends")
}

func withoutSystemFields(t *testing.T, item json.RawMessage) json.RawMessage {
	t.Helper()
	body, _, err := adapter.SplitSystemFields(item)
	require.NoError(t, err)
	return body
}

func TestDraftReplaceMovesTheVersionToTheCondition(t *testing.T) {
	a := mock.New(mock.WithItems(ordersPath, order("o1", "c01")))
	conn, err := a.Connect(context.Background(), nil)
	require.NoError(t, err)
	drafter, ok := conn.(adapter.ItemDrafter)
	require.True(t, ok)
	stored := a.Items(ordersPath)[0]

	op, err := drafter.DraftReplace(stored)

	require.NoError(t, err)
	assert.Equal(t, adapter.OperationReplace, op.Kind)
	assert.Equal(t, "o1", op.ID)
	assert.Equal(t, etagOf(t, stored), op.IfMatch)
	assert.JSONEq(t, string(order("o1", "c01")), string(op.Body))
}
