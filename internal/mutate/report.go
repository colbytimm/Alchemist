package mutate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

// MaxReportRows is how many rows a report shows; its totals always cover
// every target.
const MaxReportRows = 10000

// Leaf charge names, which the status bar splits a report's charge by.
const (
	SelectionLeaf = "selection"
	WritesLeaf    = "writes"
)

var ReportColumns = []string{"#", "id", "partitionKey", "outcome", "status", "RU"}

var errNoMorePages = errors.New("mutate: the report has no more pages")

// reportRow is the raw form of one row of a report.
type reportRow struct {
	ID            string          `json:"id"`
	PartitionKey  json.RawMessage `json:"partitionKey"`
	Outcome       string          `json:"outcome"`
	Status        string          `json:"status"`
	RequestCharge float64         `json:"requestCharge"`
	ETag          string          `json:"etag"`
}

// ReportCursor serves a job's outcome as a result set, one row per target,
// in pages. It reads a copy taken when it was made, so the job may go on
// without it.
type ReportCursor struct {
	kind     query.MutationKind
	summary  Summary
	targets  []Target
	results  []Result
	order    []int
	pageSize int
	served   int
	started  bool
}

var _ adapter.Cursor = (*ReportCursor)(nil)

// NewReportCursor lays j's outcome out in pages of pageSize: every target
// in order, or past MaxReportRows, every one not applied first and then as
// many applied ones as fit.
func NewReportCursor(j *Job, pageSize int) *ReportCursor {
	return &ReportCursor{
		kind:     j.mutation.Kind,
		summary:  j.Summary(),
		targets:  j.targets.Items,
		results:  append([]Result(nil), j.results...),
		order:    reportOrder(j.results),
		pageSize: max(pageSize, 1),
	}
}

func reportOrder(results []Result) []int {
	order := make([]int, 0, min(len(results), MaxReportRows))
	if len(results) <= MaxReportRows {
		for i := range results {
			order = append(order, i)
		}
		return order
	}
	for i, result := range results {
		if result.Outcome != Applied {
			order = append(order, i)
		}
	}
	for i, result := range results {
		if len(order) >= MaxReportRows {
			break
		}
		if result.Outcome == Applied {
			order = append(order, i)
		}
	}
	return order[:min(len(order), MaxReportRows)]
}

// Banner says what the job did, and when the report cannot show every row,
// which it shows.
func (c *ReportCursor) Banner() string {
	banner := c.summary.Sentence()
	if len(c.order) < len(c.targets) {
		banner += fmt.Sprintf("\nShowing %s of %s rows: every item that was not %s, then the first %s ones. The log names the rest.",
			formatCount(len(c.order)), formatCount(len(c.targets)), c.kind.Applied(), c.kind.Applied())
	}
	return banner
}

// FirstProblem is the row of the first target that failed or went
// unanswered.
func (c *ReportCursor) FirstProblem() (int, bool) {
	for row, i := range c.order {
		if outcome := c.results[i].Outcome; outcome == Failed || outcome == Unknown {
			return row, true
		}
	}
	return 0, false
}

func (c *ReportCursor) Summary() Summary { return c.summary }

// OmittedRow is a target the report has no row for: past MaxReportRows,
// the log names it instead.
type OmittedRow struct {
	ID           string
	PartitionKey string
	Outcome      string
}

// Omitted lists the targets past the report's cap, in target order.
func (c *ReportCursor) Omitted() []OmittedRow {
	if len(c.order) == len(c.targets) {
		return nil
	}
	shown := make([]bool, len(c.targets))
	for _, i := range c.order {
		shown[i] = true
	}
	omitted := make([]OmittedRow, 0, len(c.targets)-len(c.order))
	for i, target := range c.targets {
		if !shown[i] {
			omitted = append(omitted, OmittedRow{
				ID: target.ID, PartitionKey: query.PartitionText(target.Key), Outcome: c.results[i].Outcome.Label(c.kind),
			})
		}
	}
	return omitted
}

// NextPage serves the next rows. The first page carries the statistics of
// the whole report, and later ones none, so a caller adding up its pages
// counts every target once.
func (c *ReportCursor) NextPage(context.Context) (adapter.Page, error) {
	if c.started && !c.HasMore() {
		return adapter.Page{}, errNoMorePages
	}
	page := adapter.Page{Columns: ReportColumns}
	if !c.started {
		page.Stats = c.stats()
	}
	end := min(c.served+c.pageSize, len(c.order))
	for _, i := range c.order[c.served:end] {
		cells, raw := c.row(i)
		page.Rows = append(page.Rows, cells)
		page.Raw = append(page.Raw, raw)
	}
	c.served, c.started = end, true
	return page, nil
}

func (c *ReportCursor) HasMore() bool { return c.served < len(c.order) }

func (c *ReportCursor) Close() error { return nil }

func (c *ReportCursor) stats() adapter.Stats {
	s := c.summary
	return adapter.Stats{
		RequestCharge: s.SelectionCharge + s.WriteCharge,
		Elapsed:       s.Elapsed,
		RowCount:      s.Total,
		LeafCharges:   map[string]float64{SelectionLeaf: s.SelectionCharge, WritesLeaf: s.WriteCharge},
	}
}

func (c *ReportCursor) row(i int) ([]string, json.RawMessage) {
	target, result := c.targets[i], c.results[i]
	row := reportRow{
		ID:            target.ID,
		PartitionKey:  keyJSON(target.Key),
		Outcome:       result.Outcome.Label(c.kind),
		Status:        result.Status,
		RequestCharge: result.RequestCharge,
		ETag:          result.ETag,
	}
	raw, _ := json.Marshal(row) // strings, a number and JSON the backend served always marshal
	cells := []string{
		strconv.Itoa(i + 1), row.ID, query.PartitionText(target.Key), row.Outcome, row.Status,
		fmt.Sprintf("%.2f", row.RequestCharge),
	}
	return cells, raw
}

// keyJSON is a key as JSON: its one value, or the array of a hierarchical
// key's values, or null for none.
func keyJSON(key adapter.PartitionKey) json.RawMessage {
	switch len(key) {
	case 0:
		return json.RawMessage("null")
	case 1:
		return key[0]
	}
	values := make([]string, 0, len(key))
	for _, value := range key {
		values = append(values, string(value))
	}
	return json.RawMessage("[" + strings.Join(values, ",") + "]")
}
