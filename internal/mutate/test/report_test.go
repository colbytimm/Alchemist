package mutate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
)

// scriptedEditor answers each item as its script says, and applies the
// rest, charging 10 for each.
type scriptedEditor struct {
	script map[string]error
}

func (e scriptedEditor) EditItem(_ context.Context, _ []string, _ adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	if err, ok := e.script[op.ID]; ok {
		return adapter.OperationResult{Status: "412 Precondition Failed", RequestCharge: 1}, err
	}
	return adapter.OperationResult{Status: "200 OK", ETag: `"e-` + op.ID + `"`, RequestCharge: 10}, nil
}

func targetsOf(n int) mutate.Targets {
	targets := mutate.Targets{RequestCharge: 6.25}
	for _, id := range ids(n) {
		targets.Items = append(targets.Items, mutate.Target{ID: id, Key: adapter.PartitionKey{json.RawMessage(`"c01"`)}})
	}
	return targets
}

func finishedJob(t *testing.T, n int, script map[string]error) *mutate.Job {
	t.Helper()
	j := newJob(parse(t, archive), targetsOf(n), scriptedEditor{script: script}, 8, &fakeClock{})
	_, err := runJob(j)
	require.NoError(t, err)
	return j
}

func drainReport(t *testing.T, cursor adapter.Cursor) []adapter.Page {
	t.Helper()
	page, err := cursor.NextPage(context.Background())
	require.NoError(t, err)
	pages := []adapter.Page{page}
	for cursor.HasMore() {
		page, err := cursor.NextPage(context.Background())
		require.NoError(t, err)
		pages = append(pages, page)
	}
	return pages
}

func TestAReportHasARowForEveryTargetInPages(t *testing.T) {
	j := finishedJob(t, 5, map[string]error{"o003": adapter.ErrPreconditionFailed})
	cursor := mutate.NewReportCursor(j, 2)

	pages := drainReport(t, cursor)

	require.Len(t, pages, 3)
	assert.Equal(t, mutate.ReportColumns, pages[0].Columns)
	assert.Equal(t, []string{"1", "o000", `"c01"`, "updated", "200 OK", "10.00"}, pages[0].Rows[0])
	assert.Equal(t, []string{"4", "o003", `"c01"`, "skipped: changed", "412 Precondition Failed", "1.00"}, pages[1].Rows[1])
	assert.JSONEq(t, `{"id":"o000","partitionKey":"c01","outcome":"updated","status":"200 OK","requestCharge":10,"etag":"\"e-o000\""}`,
		string(pages[0].Raw[0]))
	assert.Equal(t, adapter.Stats{
		RequestCharge: 6.25 + 41,
		Elapsed:       j.Summary().Elapsed,
		RowCount:      5,
		LeafCharges:   map[string]float64{mutate.SelectionLeaf: 6.25, mutate.WritesLeaf: 41},
	}, pages[0].Stats, "the first page carries the whole report's statistics")
	assert.Zero(t, pages[1].Stats.RowCount)
	assert.Equal(t, "Updated 4 of 5 items in sales.orders. 1 had changed.", cursor.Banner())
	_, err := cursor.NextPage(context.Background())
	assert.Error(t, err)
}

func TestAReportPastItsCapShowsWhatWasNotAppliedFirst(t *testing.T) {
	last := fmt.Sprintf("o%03d", mutate.MaxReportRows)
	j := finishedJob(t, mutate.MaxReportRows+1, map[string]error{
		last:   fmt.Errorf("refused: %w", errors.New("400 Bad Request")),
		"o010": adapter.ErrItemNotFound,
	})
	cursor := mutate.NewReportCursor(j, mutate.MaxReportRows)

	pages := drainReport(t, cursor)

	var rows [][]string
	for _, page := range pages {
		rows = append(rows, page.Rows...)
	}
	require.Len(t, rows, mutate.MaxReportRows)
	assert.Equal(t, "o010", rows[0][1])
	assert.Equal(t, last, rows[1][1])
	assert.Equal(t, "o000", rows[2][1])
	assert.Equal(t, mutate.MaxReportRows+1, pages[0].Stats.RowCount, "totals cover every target")
	assert.Contains(t, cursor.Banner(), "Showing 10,000 of 10,001 rows: every item that was not updated, then the first updated ones.")
	row, ok := cursor.FirstProblem()
	assert.True(t, ok)
	assert.Equal(t, 1, row)
}

func TestAnUnknownRowCarriesTheAdvice(t *testing.T) {
	j := finishedJob(t, 3, map[string]error{"o001": fmt.Errorf("lost: %w", adapter.ErrWriteOutcomeUnknown)})

	pages := drainReport(t, mutate.NewReportCursor(j, 10))

	assert.Equal(t, "unknown", pages[0].Rows[1][3])
	assert.Equal(t, mutate.UnknownAdvice, pages[0].Rows[1][4])
	assert.Equal(t, "Updated 2 of 3 items in sales.orders. 1 had no answer.", mutate.NewReportCursor(j, 10).Banner())
}

func TestSummarySentence(t *testing.T) {
	tests := []struct {
		counts mutate.Counts
		want   string
	}{
		{counts: mutate.Counts{Applied: 412}, want: "Updated 412 of 412 items in sales.orders."},
		{counts: mutate.Counts{Applied: 409, Changed: 2, Gone: 1}, want: "Updated 409 of 412 items in sales.orders. 2 had changed, 1 was gone."},
		{counts: mutate.Counts{Applied: 166, Changed: 3, NotAttempted: 243}, want: "Updated 166 of 412 items in sales.orders. 3 had changed. 243 were not attempted and are unchanged."},
		{counts: mutate.Counts{Applied: 407, Gone: 2, NoKey: 1, Failed: 1, Unknown: 1}, want: "Updated 407 of 412 items in sales.orders. 2 were gone, 1 had no partition key, 1 failed, 1 had no answer."},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			summary := mutate.Summary{Kind: parse(t, archive).Kind, Container: ordersPath, Counts: tt.counts, Total: 412}

			assert.Equal(t, tt.want, summary.Sentence())
		})
	}
}
