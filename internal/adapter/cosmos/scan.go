package cosmos

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/colbytimm/alchemist/internal/adapter"
)

var _ adapter.ItemScanner = (*connection)(nil)

// scanQuery reads whole items. Unordered and across every partition, it is
// served by the gateway walking the partition key ranges in a fixed order,
// which is what makes its continuation token a stable place to resume.
const scanQuery = "SELECT * FROM c"

// ScanItems opens a scan resuming at request.From: the query's continuation
// token, as the service issued it.
func (c *connection) ScanItems(_ context.Context, request adapter.ScanRequest) (adapter.ItemScan, error) {
	op := "scan " + pathText(request.Container)
	container, err := c.containerAt(op, request.Container)
	if err != nil {
		return nil, err
	}
	options := &azcosmos.QueryOptions{PageSizeHint: request.PageSize}
	if options.PageSizeHint <= 0 {
		options.PageSizeHint = c.pageSize
	}
	if request.From != "" {
		from := string(request.From)
		options.ContinuationToken = &from
	}
	pager := container.NewQueryItemsPager(scanQuery, azcosmos.NewPartitionKey(), options)
	return &scan{op: op, pager: pager}, nil
}

type scan struct {
	op    string
	pager *runtime.Pager[azcosmos.QueryItemsResponse]
}

func (s *scan) NextPage(ctx context.Context) (adapter.ItemPage, error) {
	resp, err := s.pager.NextPage(ctx)
	if err != nil {
		return adapter.ItemPage{}, wrap(s.op, err)
	}
	page := adapter.ItemPage{RequestCharge: float64(resp.RequestCharge)}
	for _, item := range resp.Items {
		page.Items = append(page.Items, json.RawMessage(item))
	}
	if resp.ContinuationToken != nil && strings.TrimSpace(*resp.ContinuationToken) != "" {
		page.Next = adapter.ScanPosition(*resp.ContinuationToken)
	}
	return page, nil
}

func (s *scan) HasMore() bool { return s.pager.More() }

// Close releases nothing: a pager holds no connection between pages.
func (s *scan) Close() error { return nil }
