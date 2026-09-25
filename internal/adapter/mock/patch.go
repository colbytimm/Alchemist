package mock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

var errNotAnItem = errors.New("mock: patch: the stored item is not a JSON object")

type patchEntry struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// field is one top-level member of an item, kept in the order written.
type field struct {
	name  string
	value json.RawMessage
}

// patchItem applies entries to item's top-level fields, which is all the
// tests need. It answers with the status the service would give a patch it
// cannot apply, and with an error for one the mock cannot express.
func patchItem(item, entries json.RawMessage) (json.RawMessage, int, error) {
	var patch []patchEntry
	if err := json.Unmarshal(entries, &patch); err != nil {
		return nil, http.StatusBadRequest, nil
	}
	fields, err := topLevelFields(item)
	if err != nil {
		return nil, 0, err
	}
	for _, entry := range patch {
		name, ok := strings.CutPrefix(entry.Path, "/")
		if !ok || strings.Contains(name, "/") {
			return nil, 0, fmt.Errorf("mock: patch path %q: only top-level paths are supported", entry.Path)
		}
		var status int
		fields, status = applyPatch(fields, name, entry)
		if status != http.StatusOK {
			return nil, status, nil
		}
	}
	return joinFields(fields), http.StatusOK, nil
}

func applyPatch(fields []field, name string, entry patchEntry) ([]field, int) {
	i := slices.IndexFunc(fields, func(f field) bool { return f.name == name })
	switch entry.Op {
	case "add", "set":
		return setField(fields, i, field{name: name, value: entry.Value}), http.StatusOK
	case "replace":
		if i < 0 {
			return fields, http.StatusBadRequest
		}
		fields[i].value = entry.Value
	case "remove":
		if i < 0 {
			return fields, http.StatusBadRequest
		}
		return slices.Delete(fields, i, i+1), http.StatusOK
	case "incr":
		return increment(fields, i, name, entry.Value)
	default:
		return fields, http.StatusBadRequest
	}
	return fields, http.StatusOK
}

func setField(fields []field, i int, f field) []field {
	if i < 0 {
		return append(fields, f)
	}
	fields[i] = f
	return fields
}

func increment(fields []field, i int, name string, by json.RawMessage) ([]field, int) {
	step, err := strconv.ParseFloat(string(by), 64)
	if err != nil {
		return fields, http.StatusBadRequest
	}
	current := 0.0
	if i >= 0 {
		if current, err = strconv.ParseFloat(string(fields[i].value), 64); err != nil {
			return fields, http.StatusBadRequest
		}
	}
	total := json.RawMessage(strconv.FormatFloat(current+step, 'g', -1, 64))
	return setField(fields, i, field{name: name, value: total}), http.StatusOK
}

func topLevelFields(item json.RawMessage) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(item))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errNotAnItem
	}
	var fields []field
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errNotAnItem
		}
		name, _ := tok.(string) // an object's keys are always strings
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, errNotAnItem
		}
		fields = append(fields, field{name: name, value: value})
	}
	return fields, nil
}

func joinFields(fields []field) json.RawMessage {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(f.name) // a string always marshals
		out.Write(name)
		out.WriteByte(':')
		out.Write(f.value)
	}
	out.WriteByte('}')
	return out.Bytes()
}
