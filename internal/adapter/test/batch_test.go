package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

func TestPartitionKeyValues(t *testing.T) {
	tests := []struct {
		name    string
		item    string
		paths   []string
		want    []string
		wantErr error
	}{
		{name: "one string path", item: `{"id":"o1","customerId":"c01"}`, paths: []string{"/customerId"}, want: []string{`"c01"`}},
		{name: "a number keeps its spelling", item: `{"n": 1.50}`, paths: []string{"/n"}, want: []string{`1.50`}},
		{name: "a nested path", item: `{"shipTo":{"region":"eu"}}`, paths: []string{"/shipTo/region"}, want: []string{`"eu"`}},
		{name: "several paths in path order", item: `{"b":true,"a":null}`, paths: []string{"/a", "/b"}, want: []string{`null`, `true`}},
		{name: "a missing path", item: `{"a":1}`, paths: []string{"/a", "/b"}, wantErr: adapter.ErrNoPartitionKey},
		{name: "a path through a scalar", item: `{"a":1}`, paths: []string{"/a/b"}, wantErr: adapter.ErrNoPartitionKey},
		{name: "three paths in order", item: `{"c":"z","a":"x","b":{"n":"y"}}`, paths: []string{"/a", "/b/n", "/c"}, want: []string{`"x"`, `"y"`, `"z"`}},
		{name: "a boolean", item: `{"a":false}`, paths: []string{"/a"}, want: []string{`false`}},
		{name: "an explicit null is a value", item: `{"a":null}`, paths: []string{"/a"}, want: []string{`null`}},
		{name: "a number past 2^53 keeps its digits", item: `{"a":12345678901234567890}`, paths: []string{"/a"}, want: []string{`12345678901234567890`}},
		{name: "an object at the path", item: `{"a":{"b":1}}`, paths: []string{"/a"}, wantErr: adapter.ErrNoPartitionKey},
		{name: "an array at the path", item: `{"a":[1]}`, paths: []string{"/a"}, wantErr: adapter.ErrNoPartitionKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adapter.PartitionKeyValues(json.RawMessage(tt.item), tt.paths)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			var texts []string
			for _, value := range got {
				texts = append(texts, string(value))
			}
			assert.Equal(t, tt.want, texts)
		})
	}
}

func TestAMissingPartitionKeyNamesThePath(t *testing.T) {
	_, err := adapter.PartitionKeyValues(json.RawMessage(`{"a":1}`), []string{"/a", "/tenantId"})

	require.ErrorContains(t, err, "/tenantId")
}

func TestWithoutFieldsKeepsTheRestInOrder(t *testing.T) {
	got, err := adapter.WithoutFields(json.RawMessage(`{"z":1, "_etag":"x", "a":{"b": [1, 2]}, "_ts":3}`), "_etag", "_ts")

	require.NoError(t, err)
	assert.Equal(t, `{"z":1,"a":{"b":[1,2]}}`, string(got))
}

func TestWithoutFieldsRefusesANonObject(t *testing.T) {
	_, err := adapter.WithoutFields(json.RawMessage(`[1]`), "a")

	require.Error(t, err)
}

func TestOnlyAReadWritesNothing(t *testing.T) {
	for _, kind := range []adapter.OperationKind{
		adapter.OperationCreate, adapter.OperationUpsert, adapter.OperationReplace,
		adapter.OperationDelete, adapter.OperationPatch,
	} {
		assert.True(t, kind.Writes(), kind)
	}
	assert.False(t, adapter.OperationRead.Writes())
}
