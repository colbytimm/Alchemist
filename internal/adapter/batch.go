package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Batcher runs a group of item operations against one container and one
// partition key as a single transaction. Optional: callers find it with a
// comma-ok type assertion.
type Batcher interface {
	// ExecuteBatch returns a BatchResult whenever the backend answered,
	// whether it committed or rolled back. An error means no answer: it wraps
	// ErrWriteOutcomeUnknown when the batch may have been applied, and
	// otherwise guarantees that it was not. An implementation never retries.
	ExecuteBatch(ctx context.Context, b Batch) (BatchResult, error)
}

// ItemDrafter turns an item a query returned into the operation that writes
// it back: system fields removed from the body, its version tag as IfMatch.
type ItemDrafter interface {
	DraftReplace(item json.RawMessage) (Operation, error)
}

// ErrWriteOutcomeUnknown covers any write sent with no answer back: it may
// have been applied, in full, or not at all.
var ErrWriteOutcomeUnknown = errors.New("write outcome unknown")

// ErrNoPartitionKey reports an item holding nothing at one of its
// container's partition key paths.
var ErrNoPartitionKey = errors.New("no partition key value")

type Batch struct {
	Scope        []string // ["sales", "orders"]
	PartitionKey PartitionKey
	Operations   []Operation
}

// PartitionKey is one JSON scalar per key path, in path order: a string, a
// number, true, false or null. A plain key has one component.
type PartitionKey []json.RawMessage

type OperationKind string

const (
	OperationCreate  OperationKind = "create"
	OperationUpsert  OperationKind = "upsert"
	OperationReplace OperationKind = "replace"
	OperationDelete  OperationKind = "delete"
	OperationRead    OperationKind = "read"
	OperationPatch   OperationKind = "patch"
)

// Writes reports whether an operation of kind k changes what is stored.
func (k OperationKind) Writes() bool {
	return k != OperationRead
}

type Operation struct {
	Kind      OperationKind
	ID        string          // empty for create and upsert: the body carries it
	Body      json.RawMessage // an item; for a patch, the array of patch entries
	Condition string          // patch only: the filter predicate
	IfMatch   string          // version tag the target must still have
}

type BatchResult struct {
	Committed bool
	Results   []OperationResult // one per operation, in order
	Stats     Stats             // total charge, elapsed; RowCount is len(Results)
}

type OperationOutcome int

const (
	OperationApplied OperationOutcome = iota
	OperationFailed                   // the operation that caused the rollback
	OperationSkipped                  // rolled back because another one failed
)

type OperationResult struct {
	Outcome       OperationOutcome
	Status        string // the backend's words: "201 Created"
	ETag          string
	Body          json.RawMessage // what the backend returned; always set for a read
	RequestCharge float64
}

// PartitionKeyValues reads the value item holds at each of paths, written
// "/customerId" or "/shipTo/region", in order. An object or an array at a
// path is no key value, and neither is nothing at all.
func PartitionKeyValues(item json.RawMessage, paths []string) (PartitionKey, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(item, &root); err != nil {
		return nil, errNotObject
	}
	values := make(PartitionKey, 0, len(paths))
	for _, path := range paths {
		value, ok := valueAt(root, path)
		if !ok || !scalar(value) {
			return nil, fmt.Errorf("adapter: %s: %w", path, ErrNoPartitionKey)
		}
		values = append(values, value)
	}
	return values, nil
}

func valueAt(object map[string]json.RawMessage, path string) (json.RawMessage, bool) {
	names := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range names {
		value, ok := object[name]
		if !ok {
			return nil, false
		}
		if i == len(names)-1 {
			return compact(value), true
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(value, &nested) != nil {
			return nil, false
		}
		object = nested
	}
	return nil, false
}

func scalar(value json.RawMessage) bool {
	return len(value) > 0 && value[0] != '{' && value[0] != '['
}

func compact(value json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if json.Compact(&buf, value) != nil {
		return value
	}
	return buf.Bytes()
}

// WithoutFields is item with the top-level fields called names left out,
// every other field kept in the order the item wrote it.
func WithoutFields(item json.RawMessage, names ...string) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(item))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errNotObject
	}
	var kept bytes.Buffer
	kept.WriteByte('{')
	for dec.More() {
		name, value, err := nextField(dec)
		if err != nil {
			return nil, fmt.Errorf("adapter: read item: %w", err)
		}
		if slices.Contains(names, name) {
			continue
		}
		if kept.Len() > 1 {
			kept.WriteByte(',')
		}
		label, _ := json.Marshal(name) // a string always marshals
		kept.Write(label)
		kept.WriteByte(':')
		kept.Write(compact(value))
	}
	kept.WriteByte('}')
	return kept.Bytes(), nil
}

func nextField(dec *json.Decoder) (string, json.RawMessage, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", nil, err
	}
	name, ok := tok.(string)
	if !ok {
		return "", nil, errNotObject
	}
	var value json.RawMessage
	if err := dec.Decode(&value); err != nil {
		return "", nil, err
	}
	return name, value, nil
}
