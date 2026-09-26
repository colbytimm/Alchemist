package adapter

import (
	"bytes"
	"encoding/json"
)

// RenderCell renders one JSON value as a table cell: strings unquoted, null
// as empty, scalars verbatim, and objects and arrays as compact JSON.
func RenderCell(v json.RawMessage) string {
	t := bytes.TrimSpace(v)
	if len(t) == 0 {
		return ""
	}
	switch t[0] {
	case '"':
		var s string
		if err := json.Unmarshal(t, &s); err == nil {
			return s
		}
	case '{', '[':
		var buf bytes.Buffer
		if err := json.Compact(&buf, t); err == nil {
			return buf.String()
		}
	case 'n':
		return ""
	}
	return string(t)
}
