package query_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

func reportBatch() adapter.Batch {
	return ordersBatch(
		createOf(`{"id":"o900","customerId":"c01"}`),
		adapter.Operation{Kind: adapter.OperationDelete, ID: "o003"},
		readOf("o011"),
	)
}

func TestACommittedReport(t *testing.T) {
	result := adapter.BatchResult{
		Committed: true,
		Results: []adapter.OperationResult{
			{Outcome: adapter.OperationApplied, Status: "201 Created", ETag: `"e1"`, RequestCharge: 7.62},
			{Outcome: adapter.OperationApplied, Status: "204 No Content", RequestCharge: 7.43},
			{Outcome: adapter.OperationApplied, Status: "200 OK", ETag: `"e2"`, Body: json.RawMessage(`{"id":"o011"}`), RequestCharge: 1},
		},
		Stats: adapter.Stats{RequestCharge: 16.05, RowCount: 3},
	}

	page := query.BatchReport(reportBatch(), result)

	assert.Equal(t, []string{"#", "operation", "id", "outcome", "status", "RU", "etag"}, page.Columns)
	assert.Equal(t, [][]string{
		{"1", "CREATE", "o900", "applied", "201 Created", "7.62", `"e1"`},
		{"2", "DELETE", "o003", "applied", "204 No Content", "7.43", ""},
		{"3", "READ", "o011", "applied", "200 OK", "1.00", `"e2"`},
	}, page.Rows)
	assert.JSONEq(t, `{"operation":"READ","id":"o011","outcome":"applied","status":"200 OK","requestCharge":1,"etag":"\"e2\"","body":{"id":"o011"}}`,
		string(page.Raw[2]))
	assert.InDelta(t, 16.05, page.Stats.RequestCharge, 0.001)
	assert.Equal(t, 3, page.Stats.RowCount)
	assert.Equal(t, `Committed: 3 operations on sales.orders, partition "c01".`, query.BatchSummary(reportBatch(), result))
}

func TestARolledBackReport(t *testing.T) {
	skipped := adapter.OperationResult{Outcome: adapter.OperationSkipped, Status: "424 Failed Dependency"}
	result := adapter.BatchResult{Results: []adapter.OperationResult{
		skipped,
		{Outcome: adapter.OperationFailed, Status: "404 Not Found", RequestCharge: 1.24},
		skipped,
	}}

	page := query.BatchReport(reportBatch(), result)

	assert.Equal(t, []string{"2", "DELETE", "o003", "failed", "404 Not Found", "1.24", ""}, page.Rows[1])
	assert.Equal(t, "skipped", page.Rows[0][3])
	failed, ok := query.FailedOperation(result)
	require.True(t, ok)
	assert.Equal(t, 1, failed)
	assert.Equal(t, `Rolled back: nothing was written. Operation 2 (DELETE "o003") failed: 404 Not Found.`,
		query.BatchSummary(reportBatch(), result))
}

func TestAShortResultListYieldsAnErrorRow(t *testing.T) {
	result := adapter.BatchResult{Committed: true, Results: []adapter.OperationResult{{Status: "201 Created"}}}

	page := query.BatchReport(reportBatch(), result)

	require.Len(t, page.Rows, 3)
	assert.Equal(t, "error", page.Rows[2][3])
	assert.Contains(t, page.Rows[2][4], "no result")
}

func TestPartitionCheckQuery(t *testing.T) {
	tests := []struct {
		name     string
		batch    adapter.Batch
		keyPaths []string
		want     string
	}{
		{name: "one key path", batch: ordersBatch(readOf("a")), keyPaths: customerKey,
			want: `SELECT c.id, c._etag, c._ts FROM sales.orders c WHERE c.customerId = "c01"`},
		{name: "a hierarchical key", batch: adapter.Batch{Scope: []string{"t", "events"}, PartitionKey: key(`"a"`, `7`)},
			keyPaths: []string{"/tenant", "/shipTo/region"},
			want:     `SELECT c.id, c._etag, c._ts FROM t.events c WHERE c.tenant = "a" AND c.shipTo.region = 7`},
		{name: "a name that is no identifier", batch: adapter.Batch{Scope: []string{"a", "b"}, PartitionKey: key(`null`)},
			keyPaths: []string{"/device-id"}, want: `SELECT c.id, c._etag, c._ts FROM a.b c WHERE c["device-id"] = null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := query.PartitionCheckQuery(tt.batch, tt.keyPaths)

			assert.Equal(t, tt.want, got)
			_, err := query.BuildPlan(got)
			require.NoError(t, err)
		})
	}
}
