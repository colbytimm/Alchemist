package snapshot

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/canonical"
)

// maxKeyComponent bounds a component a reader will accept; a backend's own
// limits on an id and a key value are far below it.
const maxKeyComponent = 1 << 16

// Key is an item's identity: its partition key values in path order, then
// its id, each a length-prefixed canonical JSON value. A value the item
// does not have is a zero-length component, so undefined and null differ,
// as they do in Cosmos. Keys order by their bytes, which groups items by
// each component in turn.
type Key string

// Identity is a Key taken apart. A nil partition key value is undefined.
type Identity struct {
	PartitionKey []json.RawMessage
	ID           string
}

// ItemKey is the key of item, a body with or without its system fields,
// in a container partitioned on keyPaths.
func ItemKey(item json.RawMessage, keyPaths []string) (Key, error) {
	var key []byte
	for _, path := range keyPaths {
		value, err := keyValue(item, path)
		if err != nil {
			return "", err
		}
		key = appendComponent(key, value)
	}
	var head struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(item, &head); err != nil {
		return "", fmt.Errorf("snapshot: item key: %w", err)
	}
	id, err := canonicalValue(head.ID)
	if err != nil {
		return "", err
	}
	return Key(appendComponent(key, id)), nil
}

func keyValue(item json.RawMessage, path string) ([]byte, error) {
	values, err := adapter.PartitionKeyValues(item, []string{path})
	if errors.Is(err, adapter.ErrNoPartitionKey) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: item key: %w", err)
	}
	encoded, err := canonical.PartitionKeyValue(values[0])
	if err != nil {
		return nil, fmt.Errorf("snapshot: item key: %w", err)
	}
	return encoded, nil
}

func canonicalValue(value json.RawMessage) ([]byte, error) {
	if len(value) == 0 {
		return nil, nil
	}
	encoded, err := canonical.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("snapshot: item key: %w", err)
	}
	return encoded, nil
}

func appendComponent(key, component []byte) []byte {
	key = binary.AppendUvarint(key, uint64(len(component)))
	return append(key, component...)
}

// Identity takes k apart: every component but the last is a partition key
// value, and the last is the id.
func (k Key) Identity() (Identity, error) {
	var components [][]byte
	rest := []byte(k)
	for len(rest) > 0 {
		length, n := binary.Uvarint(rest)
		if n <= 0 {
			return Identity{}, fmt.Errorf("snapshot: key %q: %w", string(k), ErrCorrupt)
		}
		rest = rest[n:]
		if length > uint64(len(rest)) {
			return Identity{}, fmt.Errorf("snapshot: key %q: %w", string(k), ErrCorrupt)
		}
		components = append(components, rest[:length])
		rest = rest[length:]
	}
	if len(components) == 0 {
		return Identity{}, fmt.Errorf("snapshot: empty key: %w", ErrCorrupt)
	}
	identity := Identity{ID: idText(components[len(components)-1])}
	for _, value := range components[:len(components)-1] {
		if len(value) == 0 {
			identity.PartitionKey = append(identity.PartitionKey, nil)
			continue
		}
		identity.PartitionKey = append(identity.PartitionKey, json.RawMessage(value))
	}
	return identity, nil
}

// idText is an id as a person reads it: the string, or the JSON of
// anything else a document put there.
func idText(value []byte) string {
	var id string
	if len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &id) == nil {
		return id
	}
	return string(value)
}

// PartitionKeyText renders the partition key values as a list shows them:
// undefined as a dash, strings unquoted.
func (i Identity) PartitionKeyText() string {
	values := make([]string, len(i.PartitionKey))
	for n, value := range i.PartitionKey {
		values[n] = "-"
		if value != nil {
			values[n] = idText(value)
		}
	}
	return strings.Join(values, "/")
}
