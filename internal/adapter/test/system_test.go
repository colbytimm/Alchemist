package adapter_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
)

func TestSplitSystemFields(t *testing.T) {
	tests := []struct {
		name     string
		item     string
		wantBody string
		wantMeta adapter.ItemMeta
	}{
		{
			name:     "the five fields leave and what they said is kept",
			item:     `{"id":"a","_rid":"r","_self":"s","_etag":"\"e1\"","_attachments":"att/","_ts":1700000000,"n":1}`,
			wantBody: `{"id":"a","n":1}`,
			wantMeta: adapter.ItemMeta{Version: `"e1"`, Modified: time.Unix(1700000000, 0).UTC()},
		},
		{
			name:     "a nested field of the same name stays",
			item:     `{"id":"a","inner":{"_etag":"x","_ts":2}}`,
			wantBody: `{"id":"a","inner":{"_etag":"x","_ts":2}}`,
		},
		{
			name:     "an item with none is unchanged",
			item:     `{"id":"a","ttl":60,"big":12345678901234567890}`,
			wantBody: `{"id":"a","ttl":60,"big":12345678901234567890}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, meta, err := adapter.SplitSystemFields(json.RawMessage(tt.item))

			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, string(body))
			assert.Equal(t, tt.wantMeta, meta)
		})
	}
}

func TestSplitSystemFieldsRefusesANonObject(t *testing.T) {
	_, _, err := adapter.SplitSystemFields(json.RawMessage(`[1]`))

	require.Error(t, err)
}

func TestIsSystemFieldNamesWhatSplitRemoves(t *testing.T) {
	for _, name := range []string{"_rid", "_self", "_etag", "_attachments", "_ts"} {
		assert.True(t, adapter.IsSystemField(name), name)
	}
	for _, name := range []string{"id", "ttl", "_lsn", "etag"} {
		assert.False(t, adapter.IsSystemField(name), name)
	}
}

func TestAThrottledErrorUnwrapsItsCause(t *testing.T) {
	cause := errors.New("429 Too Many Requests")
	var err error = &adapter.ThrottledError{RetryAfter: time.Second, Err: cause}

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, err, &throttled)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, cause.Error(), err.Error())
}
