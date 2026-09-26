package adapter

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// systemFields are the top-level fields the backend writes on every item it
// stores. They are named here and nowhere else.
var systemFields = []string{"_rid", "_self", "_etag", "_attachments", "_ts"}

// ItemMeta is what a backend records about an item outside its content.
type ItemMeta struct {
	Version  string // changes on every write; the etag in Cosmos
	Modified time.Time
}

// SystemFields lists the fields IsSystemField is true for.
func SystemFields() []string {
	return slices.Clone(systemFields)
}

func IsSystemField(name string) bool {
	return slices.Contains(systemFields, name)
}

// SplitSystemFields returns item without its system fields, every other
// field kept in the order the item wrote it, and what they said.
func SplitSystemFields(item json.RawMessage) (json.RawMessage, ItemMeta, error) {
	body, err := WithoutFields(item, systemFields...)
	if err != nil {
		return nil, ItemMeta{}, err
	}
	meta, err := ReadItemMeta(item)
	return body, meta, err
}

// ReadItemMeta is what item's system fields say, without the body.
func ReadItemMeta(item json.RawMessage) (ItemMeta, error) {
	var head struct {
		ETag string `json:"_etag"`
		TS   *int64 `json:"_ts"`
	}
	if err := json.Unmarshal(item, &head); err != nil {
		return ItemMeta{}, fmt.Errorf("adapter: read system fields: %w", err)
	}
	meta := ItemMeta{Version: head.ETag}
	if head.TS != nil {
		meta.Modified = time.Unix(*head.TS, 0).UTC()
	}
	return meta, nil
}
