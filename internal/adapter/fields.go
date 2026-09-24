package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

var errNotObject = errors.New("adapter: item is not a JSON object")

// FieldSampler is optional: a Connection implements it when its backend can
// say cheaply what fields a container's items tend to have. Callers detect
// support with a comma-ok type assertion.
type FieldSampler interface {
	SampleFields(ctx context.Context, container Node) (FieldSample, error)
}

// FieldSample is what one look at a container found, and what the look cost.
type FieldSample struct {
	Fields []Field
	Stats  Stats
}

// Field is one property path observed in a container's items, dotted from
// the item root; an array's elements are under "<path>[]".
type Field struct {
	Path string // "customer.name", "tags[]", "lines[].sku"
	Kind string // "string", "number", "bool", "object", "array", "null"; "" when it varied
}

// FlattenFields lists the fields of items, in the order they were first
// seen, each with its kind unless items disagree about it. An item that is
// not a JSON object contributes nothing.
func FlattenFields(items []json.RawMessage) []Field {
	f := &flattener{kinds: map[string]string{}}
	for _, item := range items {
		f.walkItem(item)
	}
	fields := make([]Field, 0, len(f.order))
	for _, path := range f.order {
		fields = append(fields, Field{Path: path, Kind: f.kinds[path]})
	}
	return fields
}

type flattener struct {
	order []string
	kinds map[string]string
}

// walkItem reads one item token by token, so fields come out in the order
// the document wrote them. A malformed item is abandoned where it breaks.
func (f *flattener) walkItem(item json.RawMessage) {
	dec := json.NewDecoder(bytes.NewReader(item))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return
	}
	_ = f.walkObject(dec, "") // a decode error ends the walk; what came before it still counts
}

func (f *flattener) walkObject(dec *json.Decoder, prefix string) error {
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errNotObject
		}
		if err := f.walkValue(dec, prefix+key); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func (f *flattener) walkArray(dec *json.Decoder, path string) error {
	for dec.More() {
		if err := f.walkValue(dec, path); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func (f *flattener) walkValue(dec *json.Decoder, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		f.record(path, "object")
		return f.walkObject(dec, path+".")
	case json.Delim('['):
		f.record(path, "array")
		return f.walkArray(dec, path+"[]")
	}
	f.record(path, scalarKind(tok))
	return nil
}

func (f *flattener) record(path, kind string) {
	seen, ok := f.kinds[path]
	switch {
	case !ok:
		f.order = append(f.order, path)
		f.kinds[path] = kind
	case seen != kind:
		f.kinds[path] = ""
	}
}

func scalarKind(tok json.Token) string {
	switch tok.(type) {
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "bool"
	}
	return "null"
}
