package cosmos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Compile-time contract check.
var _ adapter.Cursor = (*cursor)(nil)

var ErrNotObject = errors.New("item is not a JSON object")

// cursor streams query result pages from one item pager.
type cursor struct {
	pager   *runtime.Pager[azcosmos.QueryItemsResponse]
	builder *PageBuilder
}

func newCursor(pager *runtime.Pager[azcosmos.QueryItemsResponse]) *cursor {
	return &cursor{pager: pager, builder: NewPageBuilder()}
}

func (c *cursor) NextPage(ctx context.Context) (adapter.Page, error) {
	start := time.Now()
	resp, err := c.pager.NextPage(ctx)
	if err != nil {
		return adapter.Page{}, wrap("query page", err)
	}
	page, err := c.builder.Build(resp.Items)
	if err != nil {
		return adapter.Page{}, fmt.Errorf("cosmos: shape page: %w", err)
	}
	page.Stats = adapter.Stats{
		RequestCharge: float64(resp.RequestCharge),
		Elapsed:       time.Since(start),
		RowCount:      len(page.Rows),
	}
	return page, nil
}

func (c *cursor) HasMore() bool { return c.pager.More() }

func (c *cursor) Close() error { return nil }

// PageBuilder shapes raw JSON items into table pages. Column order locks to
// the first page (union of keys in first-seen order); later pages append
// newly seen keys at the end; missing values render as empty cells.
type PageBuilder struct {
	columns []string
	seen    map[string]bool
}

func NewPageBuilder() *PageBuilder {
	return &PageBuilder{seen: map[string]bool{}}
}

// Build renders one page of raw items into rows under the accumulated
// column union.
func (b *PageBuilder) Build(items [][]byte) (adapter.Page, error) {
	cells := make([]map[string]string, 0, len(items))
	raw := make([]json.RawMessage, 0, len(items))
	for i, item := range items {
		itemCells, err := b.scanItem(item)
		if err != nil {
			return adapter.Page{}, fmt.Errorf("item %d: %w", i, err)
		}
		cells = append(cells, itemCells)
		raw = append(raw, json.RawMessage(bytes.Clone(item)))
	}
	page := adapter.Page{Columns: append([]string(nil), b.columns...), Raw: raw}
	for _, itemCells := range cells {
		row := make([]string, len(b.columns))
		for i, col := range b.columns {
			row[i] = itemCells[col]
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

func (b *PageBuilder) scanItem(item []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(item))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if tok != json.Delim('{') {
		return nil, ErrNotObject
	}
	cells := map[string]string{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("non-string key %v", keyTok)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode value of %q: %w", key, err)
		}
		if !b.seen[key] {
			b.seen[key] = true
			b.columns = append(b.columns, key)
		}
		cells[key] = renderValue(value)
	}
	return cells, nil
}

// renderValue renders one JSON value as a table cell: strings unquoted,
// null as empty, scalars verbatim, and objects/arrays as compact JSON.
func renderValue(v json.RawMessage) string {
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
