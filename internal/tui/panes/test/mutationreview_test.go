package panes_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func mutationDraft(t *testing.T, statement string, count int) panes.MutationDraft {
	t.Helper()
	m, err := query.ParseMutation(statement)
	require.NoError(t, err)
	var targets mutate.Targets
	for i := range count {
		targets.Items = append(targets.Items, mutate.Target{ID: fmt.Sprintf("o%04d", i), Key: adapter.PartitionKey{json.RawMessage(`"c01"`)}})
	}
	return panes.MutationDraft{Account: "prod", KeyPaths: []string{"/customerId"}, Mutation: m, Targets: targets, Writers: 4}
}

// flowed is a view's text with its frame and line breaks taken out, so a
// wrapped sentence reads whole.
func flowed(view string) string {
	var words []string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		words = append(words, strings.Fields(strings.Trim(line, "│╭╮╰╯─ "))...)
	}
	return strings.Join(words, " ")
}

func TestTheWholePromptShowsAtTheSmallestTerminal(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		want      string
	}{
		{
			name:      "a delete",
			statement: `DELETE FROM sales.customer_orders_archive_2025 o WHERE o.status = "cancelled"`,
			want:      "Type customer_orders_archive_2025 1204 to delete:",
		},
		{
			name:      "an update of every item",
			statement: `UPDATE sales.customer_orders_archive_2025 o SET o.flag = true WHERE true`,
			want:      "Every item in sales.customer_orders_archive_2025. Type customer_orders_archive_2025 1204 to update:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			review := panes.NewMutationReview(nil).SetSize(80, 24).Open(mutationDraft(t, tt.statement, 1204))

			view := review.View()

			assert.Contains(t, flowed(view), tt.want)
			assert.Len(t, strings.Split(view, "\n"), 24, "the review still fits the terminal")
			for _, line := range strings.Split(view, "\n") {
				assert.LessOrEqual(t, ansi.StringWidth(line), 80)
			}
		})
	}
}
