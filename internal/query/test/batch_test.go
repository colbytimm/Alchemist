package query_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

// exampleBatch is the statement the plan opens with.
const exampleBatch = `BEGIN BATCH sales.orders PARTITION "c01";
  CREATE  {"id": "o900", "customerId": "c01", "status": "open", "total": 45};
  REPLACE "o004" {"id": "o004", "customerId": "c01", "status": "shipped"}
          IF MATCH "\"0800-7f3a\"";
  PATCH   "o007" [{"op": "set", "path": "/status", "value": "cancelled"}];
  DELETE  "o003";
  READ    "o011";
COMMIT`

func key(values ...string) adapter.PartitionKey {
	var k adapter.PartitionKey
	for _, value := range values {
		k = append(k, json.RawMessage(value))
	}
	return k
}

func TestParseBatchReadsTheExample(t *testing.T) {
	got, err := query.ParseBatch(exampleBatch)

	require.NoError(t, err)
	assert.Equal(t, adapter.Batch{
		Scope:        []string{"sales", "orders"},
		PartitionKey: key(`"c01"`),
		Operations: []adapter.Operation{
			{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"o900","customerId":"c01","status":"open","total":45}`)},
			{Kind: adapter.OperationReplace, ID: "o004", Body: json.RawMessage(`{"id":"o004","customerId":"c01","status":"shipped"}`), IfMatch: `"0800-7f3a"`},
			{Kind: adapter.OperationPatch, ID: "o007", Body: json.RawMessage(`[{"op":"set","path":"/status","value":"cancelled"}]`)},
			{Kind: adapter.OperationDelete, ID: "o003"},
			{Kind: adapter.OperationRead, ID: "o011"},
		},
	}, got)
}

func TestParseBatchOperationForms(t *testing.T) {
	tests := []struct {
		name string
		op   string
		want adapter.Operation
	}{
		{name: "create", op: `CREATE {"id":"a"}`, want: adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"a"}`)}},
		{name: "upsert", op: `UPSERT {"id":"a"}`, want: adapter.Operation{Kind: adapter.OperationUpsert, Body: json.RawMessage(`{"id":"a"}`)}},
		{name: "upsert if match", op: `UPSERT {"id":"a"} IF MATCH "e1"`, want: adapter.Operation{Kind: adapter.OperationUpsert, Body: json.RawMessage(`{"id":"a"}`), IfMatch: "e1"}},
		{name: "replace", op: `REPLACE "a" {"id":"a"}`, want: adapter.Operation{Kind: adapter.OperationReplace, ID: "a", Body: json.RawMessage(`{"id":"a"}`)}},
		{name: "delete if match", op: `DELETE "a" IF MATCH "e1"`, want: adapter.Operation{Kind: adapter.OperationDelete, ID: "a", IfMatch: "e1"}},
		{name: "read", op: `READ "a"`, want: adapter.Operation{Kind: adapter.OperationRead, ID: "a"}},
		{
			name: "patch with a condition and a version",
			op:   `PATCH "a" [{"op":"incr","path":"/n","value":1}] WHERE "FROM c WHERE c.n > 10" IF MATCH "e1"`,
			want: adapter.Operation{Kind: adapter.OperationPatch, ID: "a", Body: json.RawMessage(`[{"op":"incr","path":"/n","value":1}]`),
				Condition: "FROM c WHERE c.n > 10", IfMatch: "e1"},
		},
		{name: "keywords in any case", op: `rePlace 'a' {"id":"a"} if Match 'e1'`, want: adapter.Operation{Kind: adapter.OperationReplace, ID: "a", Body: json.RawMessage(`{"id":"a"}`), IfMatch: "e1"}},
		{name: "a single-quoted id with an escaped quote", op: `READ 'it\'s'`, want: adapter.Operation{Kind: adapter.OperationRead, ID: "it's"}},
		{name: "a double-quoted id with an escaped quote", op: `READ "a\"b"`, want: adapter.Operation{Kind: adapter.OperationRead, ID: `a"b`}},
		{
			name: "a body whose strings hold ; } COMMIT and --",
			op:   `CREATE {"id":"a","note":"; } COMMIT -- ]"}`,
			want: adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"a","note":"; } COMMIT -- ]"}`)},
		},
		{
			name: "nested objects and arrays",
			op:   `CREATE {"id":"a","lines":[{"sku":[1,[2]]},{"x":{}}]}`,
			want: adapter.Operation{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"a","lines":[{"sku":[1,[2]]},{"x":{}}]}`)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := query.ParseBatch("BEGIN BATCH db.c PARTITION 1;\n" + tt.op + ";\nCOMMIT")

			require.NoError(t, err)
			require.Len(t, got.Operations, 1)
			assert.Equal(t, tt.want, got.Operations[0])
		})
	}
}

func TestParseBatchPartitionValues(t *testing.T) {
	tests := []struct {
		name      string
		partition string
		want      adapter.PartitionKey
	}{
		{name: "a string", partition: `"c01"`, want: key(`"c01"`)},
		{name: "a single-quoted string", partition: `'c01'`, want: key(`"c01"`)},
		{name: "a number", partition: `42`, want: key(`42`)},
		{name: "a negative fraction", partition: `-1.5e3`, want: key(`-1.5e3`)},
		{name: "true", partition: `TRUE`, want: key(`true`)},
		{name: "false", partition: `false`, want: key(`false`)},
		{name: "null", partition: `NULL`, want: key(`null`)},
		{name: "two values", partition: `"tenant-a", 7`, want: key(`"tenant-a"`, `7`)},
		{name: "three values", partition: `"tenant-a", "eu", null`, want: key(`"tenant-a"`, `"eu"`, `null`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := query.ParseBatch("BEGIN BATCH db.c PARTITION " + tt.partition + `; READ "a"; COMMIT`)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.PartitionKey)
		})
	}
}

func TestCommitTakesAnOptionalSemicolon(t *testing.T) {
	for _, end := range []string{"COMMIT", "COMMIT;", "commit ; \n"} {
		_, err := query.ParseBatch(`BEGIN BATCH db.c PARTITION 1; READ "a"; ` + end)
		assert.NoError(t, err, end)
	}
}

func TestParseBatchSyntaxErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		line    int
		column  int
		message string
	}{
		{name: "no COMMIT", input: "BEGIN BATCH db.c PARTITION 1;\nREAD \"a\";", line: 2, column: 10, message: "no COMMIT"},
		{name: "no ; between operations", input: "BEGIN BATCH db.c PARTITION 1;\nREAD \"a\"\nREAD \"b\";\nCOMMIT", line: 3, column: 1, message: "expected ;"},
		{name: "IF MATCH on CREATE", input: "BEGIN BATCH db.c PARTITION 1;\nCREATE {\"id\":\"a\"} IF MATCH \"e\";\nCOMMIT", line: 2, column: 19, message: "IF MATCH does not apply to CREATE"},
		{name: "IF MATCH on READ", input: "BEGIN BATCH db.c PARTITION 1;\nREAD \"a\" IF MATCH \"e\";\nCOMMIT", line: 2, column: 10, message: "IF MATCH does not apply to READ"},
		{name: "a bare container target", input: "BEGIN BATCH orders PARTITION 1; READ \"a\"; COMMIT", line: 1, column: 20, message: "not a container alone"},
		{name: "an empty batch", input: "BEGIN BATCH db.c PARTITION 1;\nCOMMIT", line: 2, column: 1, message: "at least one operation"},
		{name: "an unbalanced body", input: "BEGIN BATCH db.c PARTITION 1;\nCREATE {\"id\":\"a\";\nCOMMIT", line: 2, column: 8, message: "{ has no matching }"},
		{name: "a mismatched bracket", input: "BEGIN BATCH db.c PARTITION 1;\nCREATE {\"id\":[\"a\"};\nCOMMIT", line: 2, column: 8, message: "{ has no matching }"},
		{name: "invalid JSON in a balanced body", input: "BEGIN BATCH db.c PARTITION 1;\nCREATE {\"id\" \"a\"};\nCOMMIT", line: 2, column: 8, message: "not valid JSON"},
		{name: "text after COMMIT", input: "BEGIN BATCH db.c PARTITION 1; READ \"a\"; COMMIT; READ \"b\"", line: 1, column: 49, message: "nothing may follow COMMIT"},
		{name: "a second batch after COMMIT", input: "BEGIN BATCH db.c PARTITION 1; READ \"a\"; COMMIT\nBEGIN BATCH db.c PARTITION 1; READ \"a\"; COMMIT", line: 2, column: 1, message: "a buffer holds one batch"},
		{name: "a second batch inside the first", input: "BEGIN BATCH db.c PARTITION 1; READ \"a\";\nBEGIN BATCH db.c PARTITION 1; READ \"a\"; COMMIT", line: 2, column: 1, message: "a buffer holds one batch"},
		{name: "an unknown operation", input: "BEGIN BATCH db.c PARTITION 1; MERGE \"a\"; COMMIT", line: 1, column: 31, message: "expected CREATE"},
		{name: "a replace with no body", input: "BEGIN BATCH db.c PARTITION 1; REPLACE \"a\"; COMMIT", line: 1, column: 42, message: "expected a JSON object"},
		{name: "a bare id", input: "BEGIN BATCH db.c PARTITION 1; DELETE a; COMMIT", line: 1, column: 38, message: "quoted string"},
		{name: "an unterminated string", input: "BEGIN BATCH db.c PARTITION 1; DELETE \"a", line: 1, column: 38, message: "never closed"},
		{name: "a partition that is no scalar", input: "BEGIN BATCH db.c PARTITION {}; READ \"a\"; COMMIT", line: 1, column: 28, message: "partition key value"},
		{name: "no PARTITION", input: "BEGIN BATCH db.c; READ \"a\"; COMMIT", line: 1, column: 17, message: "expected PARTITION"},
		{name: "a number the service cannot read", input: "BEGIN BATCH db.c PARTITION 01; READ \"a\"; COMMIT", line: 1, column: 28, message: "not a number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.ParseBatch(tt.input)

			var syntax *query.BatchSyntaxError
			require.ErrorAs(t, err, &syntax)
			assert.Contains(t, syntax.Message, tt.message)
			assert.Equal(t, tt.line, syntax.Line, "line")
			assert.Equal(t, tt.column, syntax.Column, "column")
		})
	}
}

func TestIsBatch(t *testing.T) {
	assert.True(t, query.IsBatch("begin batch"))
	assert.True(t, query.IsBatch("-- a note\nBEGIN\n  BATCH db.c"))
	assert.False(t, query.IsBatch("BEGIN"))
	assert.False(t, query.IsBatch(`SELECT "BEGIN BATCH" FROM c`))
	for _, seed := range plannerSeeds {
		assert.False(t, query.IsBatch(seed), seed)
	}
}

func TestFormatOperationParsesBack(t *testing.T) {
	parsed, err := query.ParseBatch(exampleBatch)
	require.NoError(t, err)

	for _, op := range parsed.Operations {
		again, err := query.ParseBatch("BEGIN BATCH a.b PARTITION 1;\n" + query.FormatOperation(op) + ";\nCOMMIT")
		require.NoError(t, err)
		assert.Equal(t, []adapter.Operation{op}, again.Operations)
	}
}

func TestFormatBatchParsesBack(t *testing.T) {
	parsed, err := query.ParseBatch(exampleBatch)
	require.NoError(t, err)

	again, err := query.ParseBatch(query.FormatBatch(parsed))

	require.NoError(t, err)
	assert.Equal(t, parsed, again)
}

func TestAppendOperationAddsTheLastOperation(t *testing.T) {
	op := adapter.Operation{Kind: adapter.OperationDelete, ID: "o1", IfMatch: `"e"`}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "COMMIT on a line of its own",
			input: "BEGIN BATCH a.b PARTITION 1;\n  READ \"x\";\nCOMMIT",
			want:  "BEGIN BATCH a.b PARTITION 1;\n  READ \"x\";\n  DELETE \"o1\" IF MATCH \"\\\"e\\\"\";\nCOMMIT",
		},
		{
			name:  "COMMIT after the last operation",
			input: "BEGIN BATCH a.b PARTITION 1; READ \"x\"; COMMIT;",
			want:  "BEGIN BATCH a.b PARTITION 1; READ \"x\"; \n  DELETE \"o1\" IF MATCH \"\\\"e\\\"\";\nCOMMIT;",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := query.AppendOperation(tt.input, op)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppendOperationRefusesABatchThatDoesNotParse(t *testing.T) {
	_, err := query.AppendOperation("BEGIN BATCH a.b PARTITION 1; READ \"x\";", adapter.Operation{Kind: adapter.OperationRead, ID: "y"})

	var syntax *query.BatchSyntaxError
	require.ErrorAs(t, err, &syntax)
}

var batchSeeds = []string{
	exampleBatch,
	`BEGIN BATCH a.b PARTITION 'x', -2, null; UPSERT {"id":"é","v":[1e400]} IF MATCH 'e'; COMMIT;`,
	`BEGIN BATCH a.b PARTITION 1; PATCH "a" [{"op":"set","path":"/x","value":null}] WHERE "c.x > 1"; COMMIT`,
	`BEGIN BATCH a.b PARTITION 1; READ "a"`,
	`BEGIN BATCH`,
	`BEGIN BATCH a.b PARTITION 1; CREATE {"id": "a"; COMMIT`,
}

func FuzzParseBatch(f *testing.F) {
	for _, seed := range append(append([]string{}, batchSeeds...), plannerSeeds...) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		parsed, err := query.ParseBatch(input)
		if err != nil {
			return
		}
		for _, op := range parsed.Operations {
			text := "BEGIN BATCH a.b PARTITION 1;\n" + query.FormatOperation(op) + ";\nCOMMIT"
			again, err := query.ParseBatch(text)
			require.NoError(t, err, text)
			require.Equal(t, []adapter.Operation{op}, again.Operations, text)
		}
		require.True(t, strings.Contains(strings.ToUpper(input), "COMMIT"))
	})
}
