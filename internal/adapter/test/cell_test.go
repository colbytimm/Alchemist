package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/adapter"
)

func TestRenderCell(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "a string, unquoted", value: `"Lamp"`, want: "Lamp"},
		{name: "an escaped string", value: `"a \"b\""`, want: `a "b"`},
		{name: "a number, verbatim", value: `12.50`, want: "12.50"},
		{name: "a boolean", value: `true`, want: "true"},
		{name: "null, empty", value: `null`, want: ""},
		{name: "nothing, empty", value: ``, want: ""},
		{name: "an object, compacted", value: `{ "sku": "s1",  "quantity": 2 }`, want: `{"sku":"s1","quantity":2}`},
		{name: "an array, compacted", value: `[ "red", "big" ]`, want: `["red","big"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adapter.RenderCell(json.RawMessage(tt.value)))
		})
	}
}
