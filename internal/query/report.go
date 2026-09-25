package query

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// BatchReportColumns head the page a batch's outcome is shown as.
var BatchReportColumns = []string{"#", "operation", "id", "outcome", "status", "RU", "etag"}

// missingResult stands in for an operation the backend returned no result
// for, which it never should.
const missingResult = "the backend returned no result for this operation"

// reportRow is the raw form of one row of a batch report.
type reportRow struct {
	Operation     string          `json:"operation"`
	ID            string          `json:"id"`
	Outcome       string          `json:"outcome"`
	Status        string          `json:"status"`
	RequestCharge float64         `json:"requestCharge"`
	ETag          string          `json:"etag"`
	Body          json.RawMessage `json:"body,omitempty"`
}

// BatchReport lays out what became of each operation of b as a page, one
// row per operation in order, so the results pane, the detail view and
// export treat it as any other result set.
func BatchReport(b adapter.Batch, r adapter.BatchResult) adapter.Page {
	page := adapter.Page{Columns: BatchReportColumns, Stats: r.Stats}
	page.Stats.RowCount = len(b.Operations)
	for i, op := range b.Operations {
		row := reportRow{Operation: strings.ToUpper(string(op.Kind)), ID: ItemID(op), Outcome: "error", Status: missingResult}
		if i < len(r.Results) {
			result := r.Results[i]
			row.Outcome, row.Status, row.RequestCharge = outcomeText(result.Outcome), result.Status, result.RequestCharge
			row.ETag, row.Body = result.ETag, result.Body
		}
		page.Rows = append(page.Rows, []string{
			strconv.Itoa(i + 1), row.Operation, row.ID, row.Outcome, row.Status,
			fmt.Sprintf("%.2f", row.RequestCharge), row.ETag,
		})
		page.Raw = append(page.Raw, rowJSON(row))
	}
	return page
}

// rowJSON leaves out a body the backend returned malformed, which is the
// one field that can fail to marshal.
func rowJSON(row reportRow) json.RawMessage {
	raw, err := json.Marshal(row)
	if err != nil {
		row.Body = nil
		raw, _ = json.Marshal(row) // strings and a number always marshal
	}
	return raw
}

func outcomeText(outcome adapter.OperationOutcome) string {
	switch outcome {
	case adapter.OperationFailed:
		return "failed"
	case adapter.OperationSkipped:
		return "skipped"
	}
	return "applied"
}

// FailedOperation is the index of the operation that rolled r back.
func FailedOperation(r adapter.BatchResult) (int, bool) {
	for i, result := range r.Results {
		if result.Outcome == adapter.OperationFailed {
			return i, true
		}
	}
	return 0, false
}

// BatchSummary says in a sentence what became of b.
func BatchSummary(b adapter.Batch, r adapter.BatchResult) string {
	if r.Committed {
		return fmt.Sprintf("Committed: %s on %s, partition %s.",
			countOperations(len(b.Operations)), strings.Join(b.Scope, "."), PartitionText(b.PartitionKey))
	}
	summary := "Rolled back: nothing was written."
	i, ok := FailedOperation(r)
	if !ok || i >= len(b.Operations) {
		return summary
	}
	return fmt.Sprintf("%s Operation %d (%s) failed: %s.", summary, i+1, OperationName(b.Operations[i]), r.Results[i].Status)
}

// OperationName names an operation by its kind and the item it acts on:
// DELETE "o003".
func OperationName(op adapter.Operation) string {
	name := strings.ToUpper(string(op.Kind))
	if id := ItemID(op); id != "" {
		name += " " + string(jsonString(id))
	}
	return name
}

func countOperations(n int) string {
	if n == 1 {
		return "1 operation"
	}
	return fmt.Sprintf("%d operations", n)
}

// PartitionText writes a partition key the way a batch statement does.
func PartitionText(key adapter.PartitionKey) string {
	values := make([]string, 0, len(key))
	for _, value := range key {
		values = append(values, string(value))
	}
	return strings.Join(values, ", ")
}

// PartitionCheckQuery is a query over the one partition b wrote to, listing
// each item's version, for a person to run before trying b again.
func PartitionCheckQuery(b adapter.Batch, keyPaths []string) string {
	const alias = "c"
	conditions := make([]string, 0, len(keyPaths))
	for i, path := range keyPaths {
		if i >= len(b.PartitionKey) {
			break
		}
		conditions = append(conditions, fmt.Sprintf("%s = %s", propertyRef(alias, path), b.PartitionKey[i]))
	}
	text := fmt.Sprintf("SELECT %[1]s.id, %[1]s._etag, %[1]s._ts FROM %[2]s %[1]s", alias, strings.Join(b.Scope, "."))
	if len(conditions) == 0 {
		return text
	}
	return text + " WHERE " + strings.Join(conditions, " AND ")
}

// propertyRef spells a key path under alias, bracketing a name that is not
// an identifier.
func propertyRef(alias, path string) string {
	ref := alias
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if isIdentifier(name) {
			ref += "." + name
			continue
		}
		ref += "[" + string(jsonString(name)) + "]"
	}
	return ref
}

func isIdentifier(name string) bool {
	if name == "" || !isIdentStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isIdentPart(name[i]) {
			return false
		}
	}
	return !keywords[strings.ToUpper(name)]
}
