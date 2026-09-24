package cosmos

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Compile-time contract check.
var _ adapter.FieldSampler = (*connection)(nil)

// sampleSize is how many items one look at a container reads.
const sampleSize = 20

var sampleQuery = fmt.Sprintf("SELECT TOP %d * FROM c", sampleSize)

// SampleFields reads the first items of a container across every partition.
// A cross-partition query may answer its first page with no items and a
// continuation, so pages are read until one holds an item or none remain.
func (c *connection) SampleFields(ctx context.Context, container adapter.Node) (adapter.FieldSample, error) {
	op := "sample fields " + pathText(container.Path)
	if container.Kind != adapter.NodeContainer {
		return adapter.FieldSample{}, fmt.Errorf("cosmos: %s: %s node has no items: %w", op, container.Kind, adapter.ErrUnsupported)
	}
	cursor, err := c.Query(ctx, adapter.Query{Text: sampleQuery, Scope: container.Path, PageSize: sampleSize})
	if err != nil {
		return adapter.FieldSample{}, err
	}
	defer cursor.Close() //nolint:errcheck // the pager holds nothing to release
	var items []json.RawMessage
	var stats adapter.Stats
	for len(items) == 0 && cursor.HasMore() {
		page, err := cursor.NextPage(ctx)
		if err != nil {
			return adapter.FieldSample{}, err
		}
		items = append(items, page.Raw...)
		stats.RequestCharge += page.Stats.RequestCharge
		stats.Elapsed += page.Stats.Elapsed
		stats.RowCount += page.Stats.RowCount
	}
	return adapter.FieldSample{Fields: adapter.FlattenFields(items), Stats: stats}, nil
}
