//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

const batchDatabase = "alchemist_batch_it"

// batchContainer makes a scratch container keyed on paths and returns the
// connection's Batcher. The first batch is a probe: an emulator image that
// does not serve transactional batch skips the file rather than failing it.
func batchContainer(t *testing.T, name string, paths ...string) adapter.Batcher {
	t.Helper()
	conn := connectWithRetry(t)
	client := seedClient(t)
	db := freshDatabase(t, client, batchDatabase)
	definition := azcosmos.PartitionKeyDefinition{Paths: paths}
	if len(paths) > 1 {
		definition.Kind = azcosmos.PartitionKeyKindMultiHash
		definition.Version = 2
	}
	_, err := db.CreateContainer(context.Background(), azcosmos.ContainerProperties{ID: name, PartitionKeyDefinition: definition}, nil)
	require.NoError(t, err)
	batcher, ok := conn.(adapter.Batcher)
	require.True(t, ok)
	probe(t, batcher, name, len(paths))
	return batcher
}

// probe reads an item no test writes, which a batch-serving emulator
// answers with a rollback.
func probe(t *testing.T, batcher adapter.Batcher, container string, keyParts int) {
	t.Helper()
	var key adapter.PartitionKey
	for range keyParts {
		key = append(key, json.RawMessage(`"probe"`))
	}
	_, err := batcher.ExecuteBatch(context.Background(), adapter.Batch{
		Scope:        []string{batchDatabase, container},
		PartitionKey: key,
		Operations:   []adapter.Operation{{Kind: adapter.OperationRead, ID: "probe"}},
	})
	var respErr *azcore.ResponseError
	if err != nil && errors.As(err, &respErr) {
		t.Skipf("emulator image does not serve transactional batch: %d", respErr.StatusCode)
	}
}

func item(id, customer string) json.RawMessage {
	body, _ := json.Marshal(map[string]string{"id": id, "customerId": customer, "status": "open"})
	return body
}

func customerBatch(ops ...adapter.Operation) adapter.Batch {
	return adapter.Batch{
		Scope:        []string{batchDatabase, "orders"},
		PartitionKey: adapter.PartitionKey{json.RawMessage(`"c01"`)},
		Operations:   ops,
	}
}

func execute(t *testing.T, batcher adapter.Batcher, b adapter.Batch) adapter.BatchResult {
	t.Helper()
	result, err := batcher.ExecuteBatch(context.Background(), b)
	require.NoError(t, err)
	return result
}

func TestIntegrationBatch(t *testing.T) {
	batcher := batchContainer(t, "orders", "/customerId")

	t.Run("a create, an upsert and a read commit together", func(t *testing.T) {
		result := execute(t, batcher, customerBatch(
			adapter.Operation{Kind: adapter.OperationCreate, Body: item("o1", "c01")},
			adapter.Operation{Kind: adapter.OperationUpsert, Body: item("o2", "c01")},
			adapter.Operation{Kind: adapter.OperationRead, ID: "o1"},
		))

		require.True(t, result.Committed)
		assert.Contains(t, string(result.Results[2].Body), `"o1"`)
		assert.GreaterOrEqual(t, result.Stats.RequestCharge, 0.0, "request units are not implemented in the vNext emulator")
		read := execute(t, batcher, customerBatch(adapter.Operation{Kind: adapter.OperationRead, ID: "o2"}))
		assert.True(t, read.Committed)
	})

	t.Run("a create of an existing id rolls back the whole batch", func(t *testing.T) {
		result := execute(t, batcher, customerBatch(
			adapter.Operation{Kind: adapter.OperationCreate, Body: item("o3", "c01")},
			adapter.Operation{Kind: adapter.OperationDelete, ID: "o2"},
			adapter.Operation{Kind: adapter.OperationCreate, Body: item("o1", "c01")},
		))

		require.False(t, result.Committed)
		assert.Equal(t, adapter.OperationSkipped, result.Results[0].Outcome)
		assert.Equal(t, adapter.OperationSkipped, result.Results[1].Outcome)
		assert.Equal(t, adapter.OperationFailed, result.Results[2].Outcome)
		absent := execute(t, batcher, customerBatch(adapter.Operation{Kind: adapter.OperationRead, ID: "o3"}))
		assert.False(t, absent.Committed, "o3 was never written")
		kept := execute(t, batcher, customerBatch(adapter.Operation{Kind: adapter.OperationRead, ID: "o2"}))
		assert.True(t, kept.Committed, "o2 was never deleted")
	})

	t.Run("a stale IF MATCH fails with 412", func(t *testing.T) {
		result := execute(t, batcher, customerBatch(
			adapter.Operation{Kind: adapter.OperationReplace, ID: "o1", Body: item("o1", "c01"), IfMatch: `"stale"`}))

		require.False(t, result.Committed)
		assert.Equal(t, "412 Precondition Failed", result.Results[0].Status)
	})

	t.Run("a patch with a condition applies", func(t *testing.T) {
		result := execute(t, batcher, customerBatch(adapter.Operation{
			Kind: adapter.OperationPatch, ID: "o1",
			Body:      json.RawMessage(`[{"op":"set","path":"/status","value":"shipped"}]`),
			Condition: `FROM c WHERE c.status = "open"`,
		}, adapter.Operation{Kind: adapter.OperationRead, ID: "o1"}))

		require.True(t, result.Committed)
		assert.Contains(t, string(result.Results[1].Body), `"shipped"`)
	})

	t.Run("an id holding a quote round-trips", func(t *testing.T) {
		result := execute(t, batcher, customerBatch(
			adapter.Operation{Kind: adapter.OperationCreate, Body: item(`o"4`, "c01")},
			adapter.Operation{Kind: adapter.OperationRead, ID: `o"4`},
		))

		require.True(t, result.Committed)
		var read struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(result.Results[1].Body, &read))
		assert.Equal(t, `o"4`, read.ID)
	})
}

func TestIntegrationBatchOnAHierarchicalKey(t *testing.T) {
	batcher := batchContainer(t, "events", "/tenantId", "/deviceId")
	body, err := json.Marshal(map[string]string{"id": "e1", "tenantId": "t1", "deviceId": "d1"})
	require.NoError(t, err)

	result := execute(t, batcher, adapter.Batch{
		Scope:        []string{batchDatabase, "events"},
		PartitionKey: adapter.PartitionKey{json.RawMessage(`"t1"`), json.RawMessage(`"d1"`)},
		Operations: []adapter.Operation{
			{Kind: adapter.OperationCreate, Body: body},
			{Kind: adapter.OperationRead, ID: "e1"},
		},
	})

	assert.True(t, result.Committed)
}
