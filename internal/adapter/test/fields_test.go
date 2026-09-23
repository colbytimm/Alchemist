package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/adapter"
)

func items(docs ...string) []json.RawMessage {
	raw := make([]json.RawMessage, 0, len(docs))
	for _, doc := range docs {
		raw = append(raw, json.RawMessage(doc))
	}
	return raw
}

func TestFlattenFields(t *testing.T) {
	tests := []struct {
		name string
		docs []string
		want []adapter.Field
	}{
		{
			name: "scalars in document order",
			docs: []string{`{"id": "1", "total": 9.5, "paid": true, "note": null}`},
			want: []adapter.Field{
				{Path: "id", Kind: "string"}, {Path: "total", Kind: "number"},
				{Path: "paid", Kind: "bool"}, {Path: "note", Kind: "null"},
			},
		},
		{
			name: "nested objects",
			docs: []string{`{"customer": {"name": "Ann", "address": {"city": "Oslo"}}}`},
			want: []adapter.Field{
				{Path: "customer", Kind: "object"}, {Path: "customer.name", Kind: "string"},
				{Path: "customer.address", Kind: "object"}, {Path: "customer.address.city", Kind: "string"},
			},
		},
		{
			name: "arrays of objects",
			docs: []string{`{"lines": [{"sku": "a", "qty": 1}, {"sku": "b"}]}`},
			want: []adapter.Field{
				{Path: "lines", Kind: "array"}, {Path: "lines[]", Kind: "object"},
				{Path: "lines[].sku", Kind: "string"}, {Path: "lines[].qty", Kind: "number"},
			},
		},
		{
			name: "arrays of scalars",
			docs: []string{`{"tags": ["a", "b"]}`},
			want: []adapter.Field{{Path: "tags", Kind: "array"}, {Path: "tags[]", Kind: "string"}},
		},
		{
			name: "a kind that varies across documents is left open",
			docs: []string{`{"n": 1}`, `{"n": "one"}`},
			want: []adapter.Field{{Path: "n", Kind: ""}},
		},
		{
			name: "a field seen twice appears once, where it was first seen",
			docs: []string{`{"a": 1, "b": 2}`, `{"b": 3, "c": 4, "a": 5}`},
			want: []adapter.Field{{Path: "a", Kind: "number"}, {Path: "b", Kind: "number"}, {Path: "c", Kind: "number"}},
		},
		{name: "an empty document", docs: []string{`{}`}, want: []adapter.Field{}},
		{name: "no documents", docs: nil, want: []adapter.Field{}},
		{
			name: "a value that is not an object contributes nothing",
			docs: []string{`"just a string"`, `[1, 2]`, `{"id": "1"}`},
			want: []adapter.Field{{Path: "id", Kind: "string"}},
		},
		{
			name: "a malformed document counts up to where it breaks",
			docs: []string{`{"id": "1", "total": `},
			want: []adapter.Field{{Path: "id", Kind: "string"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adapter.FlattenFields(items(tt.docs...)))
		})
	}
}
