package query_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

var customerKey = []string{"/customerId"}

func ordersBatch(ops ...adapter.Operation) adapter.Batch {
	return adapter.Batch{Scope: []string{"sales", "orders"}, PartitionKey: key(`"c01"`), Operations: ops}
}

func createOf(body string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(body)}
}

func readOf(id string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationRead, ID: id}
}

func patchOf(entries string) adapter.Operation {
	return adapter.Operation{Kind: adapter.OperationPatch, ID: "o1", Body: json.RawMessage(entries), IfMatch: "e"}
}

func reads(n int) []adapter.Operation {
	ops := make([]adapter.Operation, n)
	for i := range ops {
		ops[i] = readOf(fmt.Sprintf("o%d", i))
	}
	return ops
}

func TestCheckBatchProblems(t *testing.T) {
	tests := []struct {
		name     string
		batch    adapter.Batch
		keyPaths []string
		want     string
	}{
		{name: "one past the operation limit", batch: ordersBatch(reads(101)...), keyPaths: customerKey,
			want: "a batch takes at most 100 operations; this one has 101"},
		{name: "past the size limit", batch: ordersBatch(createOf(`{"id":"a","customerId":"c01","pad":"` + strings.Repeat("x", query.MaxBatchBytes) + `"}`)),
			keyPaths: customerKey, want: "about 2.0 MB; the service takes 2.0 MB"},
		{name: "too few key values", batch: ordersBatch(readOf("a")), keyPaths: []string{"/tenantId", "/deviceId"},
			want: "sales.orders is keyed on /tenantId,/deviceId: give 2 values"},
		{name: "a body with no id", batch: ordersBatch(createOf(`{"customerId":"c01"}`)), keyPaths: customerKey,
			want: `operation 1 (CREATE): body has no "id"`},
		{name: "a body that is no object", batch: ordersBatch(adapter.Operation{Kind: adapter.OperationUpsert, Body: json.RawMessage(`[1]`)}),
			keyPaths: customerKey, want: "operation 1 (UPSERT): body is not a JSON object"},
		{name: "a replace body of another id", batch: ordersBatch(adapter.Operation{Kind: adapter.OperationReplace, ID: "o004",
			Body: json.RawMessage(`{"id":"o005","customerId":"c01"}`), IfMatch: "e"}), keyPaths: customerKey,
			want: `operation 1 (REPLACE "o004"): replaces "o004" with a body whose id is "o005"`},
		{name: "a body in another partition", batch: ordersBatch(createOf(`{"id":"a","customerId":"c02"}`)), keyPaths: customerKey,
			want: `operation 1 (CREATE): body has customerId "c02"; the batch is for "c01"`},
		{name: "a body missing its key", batch: ordersBatch(createOf(`{"id":"a"}`)), keyPaths: customerKey,
			want: "operation 1 (CREATE): body has no customerId"},
		{name: "an empty patch", batch: ordersBatch(patchOf(`[]`)), keyPaths: customerKey,
			want: `operation 1 (PATCH "o1"): a patch is a non-empty array`},
		{name: "an unknown patch op", batch: ordersBatch(patchOf(`[{"op":"merge","path":"/x","value":1}]`)), keyPaths: customerKey,
			want: `patch op "merge" is not one of`},
		{name: "a patch entry with no path", batch: ordersBatch(patchOf(`[{"op":"set","value":1}]`)), keyPaths: customerKey,
			want: "set has no path"},
		{name: "a patch of the id", batch: ordersBatch(patchOf(`[{"op":"set","path":"/id","value":"x"}]`)), keyPaths: customerKey,
			want: "cannot patch /id"},
		{name: "a patch of the key", batch: ordersBatch(patchOf(`[{"op":"set","path":"/customerId","value":"x"}]`)), keyPaths: customerKey,
			want: "cannot patch the partition key /customerId"},
		{name: "a patch entry with no value", batch: ordersBatch(patchOf(`[{"op":"set","path":"/status"}]`)), keyPaths: customerKey,
			want: "set /status has no value"},
		{name: "two creates of one id", batch: ordersBatch(createOf(`{"id":"o9","customerId":"c01"}`), readOf("x"), createOf(`{"id":"o9","customerId":"c01"}`)),
			keyPaths: customerKey, want: `operations 1 and 3 both create "o9"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := query.CheckBatch(tt.batch, tt.keyPaths)

			require.Len(t, got.Problems, 1, "%q", got.Problems)
			assert.Contains(t, got.Problems[0], tt.want)
		})
	}
}

func TestCheckBatchAdmitsWhatIsAtTheLimits(t *testing.T) {
	atCount := query.CheckBatch(ordersBatch(reads(query.MaxBatchOperations)...), customerKey)
	assert.Empty(t, atCount.Problems)

	pad := query.MaxBatchBytes - query.EstimateBytes(ordersBatch(createOf(`{"id":"a","customerId":"c01","pad":""}`)))
	atSize := ordersBatch(createOf(`{"id":"a","customerId":"c01","pad":"` + strings.Repeat("x", pad) + `"}`))
	require.Equal(t, query.MaxBatchBytes, query.EstimateBytes(atSize))
	assert.Empty(t, query.CheckBatch(atSize, customerKey).Problems)

	onePast := ordersBatch(createOf(`{"id":"a","customerId":"c01","pad":"` + strings.Repeat("x", pad+1) + `"}`))
	assert.Len(t, query.CheckBatch(onePast, customerKey).Problems, 1)
}

func TestCheckBatchReportsEveryProblem(t *testing.T) {
	got := query.CheckBatch(ordersBatch(
		createOf(`{"id":"o900","customerId":"c02"}`),
		adapter.Operation{Kind: adapter.OperationReplace, ID: "o004", Body: json.RawMessage(`{"customerId":"c01"}`)},
		createOf(`{"id":"o900","customerId":"c01"}`),
	), customerKey)

	assert.Equal(t, []string{
		`operation 1 (CREATE): body has customerId "c02"; the batch is for "c01"`,
		`operation 2 (REPLACE "o004"): body has no "id"`,
		`operations 1 and 3 both create "o900"`,
	}, got.Problems)
}

func TestCheckBatchComparesKeysByValueAndType(t *testing.T) {
	tests := []struct {
		name     string
		keyPaths []string
		key      adapter.PartitionKey
		body     string
		ok       bool
	}{
		{name: "1 matches 1.0", keyPaths: []string{"/n"}, key: key(`1`), body: `{"id":"a","n":1.0}`, ok: true},
		{name: "1 matches 1e0", keyPaths: []string{"/n"}, key: key(`1`), body: `{"id":"a","n":1e0}`, ok: true},
		{name: "1 does not match \"1\"", keyPaths: []string{"/n"}, key: key(`1`), body: `{"id":"a","n":"1"}`},
		{name: "a nested path", keyPaths: []string{"/shipTo/region"}, key: key(`"eu"`), body: `{"id":"a","shipTo":{"region":"eu"}}`, ok: true},
		{name: "a hierarchical key with its second value wrong", keyPaths: []string{"/t", "/d"}, key: key(`"a"`, `"x"`), body: `{"id":"a","t":"a","d":"y"}`},
		{name: "a hierarchical key that matches", keyPaths: []string{"/t", "/d"}, key: key(`"a"`, `"x"`), body: `{"id":"a","t":"a","d":"x"}`, ok: true},
		{name: "a null key", keyPaths: []string{"/n"}, key: key(`null`), body: `{"id":"a","n":null}`, ok: true},
		{name: "a boolean key", keyPaths: []string{"/n"}, key: key(`true`), body: `{"id":"a","n":true}`, ok: true},
		{name: "false is not null", keyPaths: []string{"/n"}, key: key(`null`), body: `{"id":"a","n":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := adapter.Batch{Scope: []string{"a", "b"}, PartitionKey: tt.key, Operations: []adapter.Operation{createOf(tt.body)}}

			got := query.CheckBatch(b, tt.keyPaths)

			assert.Equal(t, tt.ok, len(got.Problems) == 0, "%q", got.Problems)
		})
	}
}

func TestCheckBatchWarnings(t *testing.T) {
	tests := []struct {
		name  string
		batch adapter.Batch
		want  string
	}{
		{name: "a replace with no IF MATCH", batch: ordersBatch(adapter.Operation{Kind: adapter.OperationReplace, ID: "o4", Body: json.RawMessage(`{"id":"o4","customerId":"c01"}`)}),
			want: "REPLACE o4 has no IF MATCH: it replaces whatever is there now"},
		{name: "a delete with no IF MATCH", batch: ordersBatch(adapter.Operation{Kind: adapter.OperationDelete, ID: "o3"}),
			want: "DELETE o3 has no IF MATCH: it deletes whatever is there now"},
		{name: "a patch with no IF MATCH", batch: ordersBatch(adapter.Operation{Kind: adapter.OperationPatch, ID: "o7", Body: json.RawMessage(`[{"op":"remove","path":"/x"}]`)}),
			want: "PATCH o7 has no IF MATCH: it patches whatever is there now"},
		{name: "a create then a patch of one id", batch: ordersBatch(createOf(`{"id":"o1","customerId":"c01"}`), patchOf(`[{"op":"remove","path":"/x"}]`)),
			want: `operations 1 and 2 both act on "o1"`},
		{name: "close to the operation limit", batch: ordersBatch(reads(90)...), want: "90 operations: close to the service's limit of 100"},
		{name: "close to the size limit", batch: ordersBatch(createOf(`{"id":"a","customerId":"c01","pad":"` + strings.Repeat("x", query.MaxBatchBytes*95/100) + `"}`)),
			want: "close to the service's limit of 2.0 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := query.CheckBatch(tt.batch, customerKey)

			assert.Empty(t, got.Problems)
			require.Len(t, got.Warnings, 1, "%q", got.Warnings)
			assert.Contains(t, got.Warnings[0], tt.want)
		})
	}
}

func TestANullPatchValueIsAValue(t *testing.T) {
	got := query.CheckBatch(ordersBatch(patchOf(`[{"op":"set","path":"/x","value":null},{"op":"remove","path":"/y"}]`)), customerKey)

	assert.Empty(t, got.Problems)
}
