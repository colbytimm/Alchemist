package mutate_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
)

const deleteShipped = `DELETE FROM sales.orders o WHERE ` + shipped

func storedIDs(a *mock.Adapter) []string {
	var listed []string
	for _, item := range a.Items(ordersPath) {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(item, &head) == nil {
			listed = append(listed, head.ID)
		}
	}
	return listed
}

func TestEveryTargetGetsOneDeleteOnItsSelectedVersion(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, n int) {
		a := store(append(orders(250, "shipped"), order(250, "open")))
		m, targets := mustSelect(t, a, deleteShipped)
		require.Len(t, targets.Items, 250)
		versions := map[string]string{}
		for _, target := range targets.Items {
			require.NotEmpty(t, target.Version)
			versions[target.ID] = target.Version
		}
		editor := &recordingEditor{ItemEditor: editorOf(t, a)}

		progress, err := runJob(newJob(m, targets, editor, n, &fakeClock{}))

		require.NoError(t, err)
		assert.Equal(t, mutate.Counts{Applied: 250}, progress.Counts)
		sent := editor.sent()
		require.Len(t, sent, 250)
		seen := map[string]int{}
		for _, op := range sent {
			seen[op.ID]++
			assert.Equal(t, adapter.OperationDelete, op.Kind)
			assert.Equal(t, versions[op.ID], op.IfMatch)
			assert.Empty(t, op.Condition)
			assert.Empty(t, op.Body)
		}
		assert.Len(t, seen, 250, "each target once")
		assert.Equal(t, []string{"o250"}, storedIDs(a))
		assert.LessOrEqual(t, a.HighestConcurrentEdits(), n)
	})
}

func TestAnyWriteSinceTheSelectionKeepsTheItemFromADelete(t *testing.T) {
	changed := json.RawMessage(`{"id":"o001","customerId":"c01","status":"shipped","total":1,"memo":"new"}`)
	tests := []struct {
		name      string
		statement string
		want      mutate.Counts
	}{
		{name: "a delete keeps it", statement: deleteShipped, want: mutate.Counts{Applied: 2, Changed: 1}},
		{name: "an update, whose guard is its WHERE, still writes it", statement: archive, want: mutate.Counts{Applied: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := store(orders(3, "shipped"))
			m, targets := mustSelect(t, a, tt.statement)
			require.NoError(t, a.PutItem(ordersPath, changed), "a change the WHERE does not read: it still matches")

			progress, err := runJob(newJob(m, targets, editorOf(t, a), 1, &fakeClock{}))

			require.NoError(t, err)
			assert.Equal(t, tt.want, progress.Counts)
			assert.Equal(t, `"new"`, storedField(t, a, "o001", "memo"))
		})
	}
}

func TestAChangedItemIsReportedAsKept(t *testing.T) {
	a := store(orders(3, "shipped"))
	m, targets := mustSelect(t, a, deleteShipped)
	require.NoError(t, a.PutItem(ordersPath, order(1, "shipped")))
	j := newJob(m, targets, editorOf(t, a), 1, &fakeClock{})

	_, err := runJob(j)

	require.NoError(t, err)
	assert.Equal(t, []string{"o001"}, storedIDs(a))
	cursor := mutate.NewReportCursor(j, 10)
	assert.Equal(t, "Deleted 2 of 3 items from sales.orders. 1 had changed and was kept.", cursor.Banner())
	page, err := cursor.NextPage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "deleted", page.Rows[0][3])
	assert.Equal(t, "skipped: changed", page.Rows[1][3])
	assert.Equal(t, "412 Precondition Failed", page.Rows[1][4])
}

func TestAnItemDeletedSinceTheSelectionIsGoneAndNotAFailure(t *testing.T) {
	a := store(orders(3, "shipped"))
	m, targets := mustSelect(t, a, deleteShipped)
	require.NoError(t, a.DeleteItem(ordersPath, "o002", adapter.PartitionKey{json.RawMessage(`"c02"`)}))
	j := newJob(m, targets, editorOf(t, a), 1, &fakeClock{})

	progress, err := runJob(j)

	require.NoError(t, err)
	assert.Equal(t, mutate.Counts{Applied: 2, Gone: 1}, progress.Counts)
	assert.True(t, j.Summary().Clean())
	assert.Empty(t, storedIDs(a))
}

func TestAnUnansweredDeleteIsNeverSentAgainAndARerunSettlesIt(t *testing.T) {
	a := store(orders(5, "shipped"), mock.WithEditUnknown("o002"))
	m, targets := mustSelect(t, a, deleteShipped)

	progress, err := runJob(newJob(m, targets, editorOf(t, a), 1, &fakeClock{}))

	require.NoError(t, err)
	assert.Equal(t, mutate.Counts{Applied: 4, Unknown: 1}, progress.Counts)
	assert.Equal(t, []string{"o000", "o001", "o002", "o003", "o004"}, a.EditedIDs(), "o002 once")
	_, again := mustSelect(t, a, deleteShipped)
	assert.Empty(t, again.Items, "the mock applied it before losing the answer, so the rerun has nothing left")
}

func TestARerunAfterAStopSelectsWhatWasNotDeleted(t *testing.T) {
	a := store(orders(250, "shipped"))
	m, targets := mustSelect(t, a, deleteShipped)
	j := newJob(m, targets, editorOf(t, a), 4, &fakeClock{})
	for range 2 {
		_, err := j.ApplyChunk(context.Background())
		require.NoError(t, err)
	}
	progress, err := j.ApplyChunk(cancelled())
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 149, progress.Counts.NotAttempted)

	_, rest := mustSelect(t, a, deleteShipped)

	var left []string
	for _, target := range rest.Items {
		left = append(left, target.ID)
	}
	assert.ElementsMatch(t, ids(250)[101:], left)
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestADeleteTargetWithoutAVersionIsNeverSent(t *testing.T) {
	a := store(orders(2, "shipped"))
	m := parse(t, deleteShipped)
	targets := mutate.Targets{Items: []mutate.Target{
		{ID: "o000", Key: adapter.PartitionKey{json.RawMessage(`"c00"`)}},
		{ID: "o001", Key: adapter.PartitionKey{json.RawMessage(`"c01"`)}, Version: "*"},
	}}

	progress, err := runJob(newJob(m, targets, editorOf(t, a), 1, &fakeClock{}))

	require.NoError(t, err)
	assert.Equal(t, 1, progress.Counts.Failed)
	assert.Equal(t, []string{"o001"}, a.EditedIDs())
	assert.Contains(t, storedIDs(a), "o000")
}

func TestARefusedFirstDeleteEndsTheJob(t *testing.T) {
	a := store(orders(5, "shipped"), mock.WithEditRefusal("o000"))
	m, targets := mustSelect(t, a, deleteShipped)

	progress, err := runJob(newJob(m, targets, editorOf(t, a), 4, &fakeClock{}))

	require.ErrorIs(t, err, mutate.ErrProbeRefused)
	assert.NotContains(t, err.Error(), "patch", "a delete is no patch")
	assert.Equal(t, 4, progress.Counts.NotAttempted)
	assert.Len(t, storedIDs(a), 5)
}

func TestConfirmation(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		want      string
	}{
		{name: "an update: the name", statement: archive, want: "orders"},
		{name: "an update of every item: the name and the count", statement: `UPDATE sales.orders o SET o.x = 1 WHERE true`, want: "orders 20"},
		{name: "a delete: the name and the count", statement: deleteShipped, want: "orders 20"},
		{name: "a delete of every item: the name and the count", statement: `DELETE FROM sales.orders o WHERE true`, want: "orders 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mutate.Confirmation(parse(t, tt.statement), 20))
		})
	}
}

func TestPlanningCharge(t *testing.T) {
	assert.Equal(t, mutate.PlanningChargePerDelete, mutate.PlanningCharge(query.MutationDelete))
	assert.Equal(t, mutate.PlanningChargePerPatch, mutate.PlanningCharge(query.MutationUpdate))
}

func TestADeleteSummaryInWords(t *testing.T) {
	tests := []struct {
		name   string
		counts mutate.Counts
		want   string
	}{
		{name: "every item", counts: mutate.Counts{Applied: 20}, want: "Deleted 20 of 20 items from sales.orders."},
		{
			name: "two kept", counts: mutate.Counts{Applied: 17, Changed: 2, Gone: 1},
			want: "Deleted 17 of 20 items from sales.orders. 2 had changed and were kept, 1 was gone.",
		},
		{
			name: "stopped", counts: mutate.Counts{Applied: 5, NotAttempted: 15},
			want: "Deleted 5 of 20 items from sales.orders. 15 were not attempted and are still there.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := mutate.Summary{Kind: query.MutationDelete, Container: ordersPath, Counts: tt.counts, Total: 20}

			assert.Equal(t, tt.want, summary.Sentence())
		})
	}
}
