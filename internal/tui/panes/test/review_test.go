package panes_test

import (
	"encoding/json"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func reviewOf(ops ...adapter.Operation) panes.BatchReview {
	b := adapter.Batch{
		Scope:        []string{"sales", "orders"},
		PartitionKey: adapter.PartitionKey{json.RawMessage(`"c01"`)},
		Operations:   ops,
	}
	keyPaths := []string{"/customerId"}
	draft := panes.BatchDraft{Account: "prod", KeyPaths: keyPaths, Batch: b, Check: query.CheckBatch(b, keyPaths)}
	return panes.NewBatchReview(nil).SetSize(dialogWidth, dialogHeight).Open(draft)
}

func exampleOperations() []adapter.Operation {
	return []adapter.Operation{
		{Kind: adapter.OperationCreate, Body: json.RawMessage(`{"id":"o900","customerId":"c01"}`)},
		{Kind: adapter.OperationReplace, ID: "o004", Body: json.RawMessage(`{"id":"o004","customerId":"c01"}`), IfMatch: `"0800-7f3a"`},
		{Kind: adapter.OperationPatch, ID: "o007", Body: json.RawMessage(`[{"op":"set","path":"/status","value":"x"}]`), IfMatch: `"e"`},
		{Kind: adapter.OperationDelete, ID: "o003"},
		{Kind: adapter.OperationRead, ID: "o011"},
	}
}

func typeIntoReview(r panes.BatchReview, text string) panes.BatchReview {
	r, _ = r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return r
}

func TestTheReviewSaysWhereEveryOperationGoes(t *testing.T) {
	view := plain(reviewOf(exampleOperations()...).View())

	for _, want := range []string{
		" Review batch ", "Account        prod", "Container      sales.orders", `Partition key  /customerId = "c01"`,
		"5 (1 create, 1 replace, 1 patch, 1 delete, 1 read)",
		"1  CREATE   o900", `2  REPLACE  o004  if match "0800-7f3a"`, "3  PATCH    o007  set /status", "4  DELETE   o003", "5  READ     o011",
		"! DELETE o003 has no IF MATCH", "All of it commits or none of it does. There is no undo.", "Type the container name to commit:",
	} {
		assert.Contains(t, view, want)
	}
}

func TestTheReviewIsConfirmedByTheExactContainerName(t *testing.T) {
	tests := []struct {
		typed string
		want  bool
	}{
		{typed: "", want: false},
		{typed: "Orders", want: false},
		{typed: "orders ", want: false},
		{typed: "sales.orders", want: false},
		{typed: "orders", want: true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.typed), func(t *testing.T) {
			assert.Equal(t, tt.want, typeIntoReview(reviewOf(exampleOperations()...), tt.typed).Confirmed())
		})
	}
}

func TestTheReviewFillsItsFrameExactly(t *testing.T) {
	view := reviewOf(exampleOperations()...).View()

	assert.Equal(t, dialogWidth, lipgloss.Width(view))
	assert.Equal(t, dialogHeight, lipgloss.Height(view))
}

func TestALongReviewScrollsItsListAndKeepsTheNameField(t *testing.T) {
	var ops []adapter.Operation
	for i := range 40 {
		ops = append(ops, adapter.Operation{Kind: adapter.OperationRead, ID: fmt.Sprintf("r%02d", i)})
	}
	r := reviewOf(ops...)
	for range 60 {
		r = r.ScrollDown()
	}

	view := plain(r.View())
	assert.NotContains(t, view, "r00")
	assert.Contains(t, view, "r39")
	assert.Contains(t, view, "Type the container name to commit:")

	for range 60 {
		r = r.ScrollUp()
	}
	assert.Contains(t, plain(r.View()), "r00")
}
