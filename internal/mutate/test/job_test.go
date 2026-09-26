package mutate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/mutate"
)

func TestEveryTargetGetsOneConditionalPatch(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, n int) {
		a := store(append(orders(250, "shipped"), order(250, "open")))
		m, targets := mustSelect(t, a, archive)
		editor := &recordingEditor{ItemEditor: editorOf(t, a)}

		progress, err := runJob(newJob(m, targets, editor, n, &fakeClock{}))

		require.NoError(t, err)
		assert.True(t, progress.Done)
		assert.Equal(t, mutate.Counts{Applied: 250}, progress.Counts)
		sent := editor.sent()
		require.Len(t, sent, 250)
		seen := map[string]int{}
		for _, op := range sent {
			seen[op.ID]++
			assert.Equal(t, adapter.OperationPatch, op.Kind)
			assert.Equal(t, `FROM o WHERE (o.status = "shipped")`, op.Condition)
			assert.JSONEq(t, `[{"op":"set","path":"/status","value":"archived"}]`, string(op.Body))
		}
		assert.Len(t, seen, 250, "each target once")
		assert.Equal(t, `"open"`, storedField(t, a, "o250", "status"))
		assert.Equal(t, `"archived"`, storedField(t, a, "o249", "status"))
		assert.LessOrEqual(t, a.HighestConcurrentEdits(), n)
	})
}

func TestAnUnsetIsSentOnlyWhereThePathWasAndMustStillBe(t *testing.T) {
	a := store([]json.RawMessage{
		json.RawMessage(`{"id":"with","customerId":"c01","status":"shipped","note":"x"}`),
		json.RawMessage(`{"id":"without","customerId":"c01","status":"shipped"}`),
	})
	m, targets := mustSelect(t, a, `UPDATE sales.orders o SET o.done = true UNSET o.note WHERE `+shipped)
	editor := &recordingEditor{ItemEditor: editorOf(t, a)}

	_, err := runJob(newJob(m, targets, editor, 1, &fakeClock{}))

	require.NoError(t, err)
	sent := editor.sent()
	require.Len(t, sent, 2)
	assert.JSONEq(t, `[{"op":"set","path":"/done","value":true},{"op":"remove","path":"/note"}]`, string(sent[0].Body))
	assert.Equal(t, `FROM o WHERE (o.status = "shipped") AND IS_DEFINED(o.note)`, sent[0].Condition)
	assert.JSONEq(t, `[{"op":"set","path":"/done","value":true}]`, string(sent[1].Body))
	assert.Equal(t, `FROM o WHERE (o.status = "shipped")`, sent[1].Condition)
	assert.Empty(t, storedField(t, a, "with", "note"))
}

func TestWhatChangedSinceTheSelectionDecidesEachWrite(t *testing.T) {
	a := store(orders(3, "shipped"))
	m, targets := mustSelect(t, a, archive)
	require.NoError(t, a.PutItem(ordersPath, order(0, "cancelled")))
	require.NoError(t, a.PutItem(ordersPath, json.RawMessage(`{"id":"o001","customerId":"c01","status":"shipped","total":1,"memo":"kept"}`)))
	require.NoError(t, a.DeleteItem(ordersPath, "o002", adapter.PartitionKey{json.RawMessage(`"c02"`)}))
	j := newJob(m, targets, editorOf(t, a), 1, &fakeClock{})

	progress, err := runJob(j)

	require.NoError(t, err)
	assert.Equal(t, mutate.Counts{Applied: 1, Changed: 1, Gone: 1}, progress.Counts)
	assert.Equal(t, `"cancelled"`, storedField(t, a, "o000", "status"), "an item that stopped matching is untouched")
	assert.Equal(t, `"archived"`, storedField(t, a, "o001", "status"))
	assert.Equal(t, `"kept"`, storedField(t, a, "o001", "memo"), "a change to another field survives the patch")
	assert.True(t, j.Summary().Clean(), "skips are expected")
}

func TestAKeylessTargetIsNeverWritten(t *testing.T) {
	a := store([]json.RawMessage{order(0, "shipped"), json.RawMessage(`{"id":"keyless","status":"shipped"}`)})
	m, targets := mustSelect(t, a, archive)

	progress, err := runJob(newJob(m, targets, editorOf(t, a), 1, &fakeClock{}))

	require.NoError(t, err)
	assert.Equal(t, mutate.Counts{Applied: 1, NoKey: 1}, progress.Counts)
	assert.Equal(t, []string{"o000"}, a.EditedIDs())
}

func TestTheFirstWriteIsAProbe(t *testing.T) {
	tests := []struct {
		name    string
		refused string
		calls   int
		ended   bool
	}{
		{name: "a refusal of the first write ends the job", refused: "o000", calls: 1, ended: true},
		{name: "a refusal later does not", refused: "o050", calls: 120},
	}
	for _, tt := range tests {
		eachWriterCount(t, func(t *testing.T, n int) {
			t.Run(tt.name, func(t *testing.T) {
				a := store(orders(120, "shipped"), mock.WithEditRefusal(tt.refused))
				m, targets := mustSelect(t, a, archive)

				progress, err := runJob(newJob(m, targets, editorOf(t, a), n, &fakeClock{}))

				assert.Len(t, a.EditedIDs(), tt.calls)
				if tt.ended {
					require.ErrorIs(t, err, mutate.ErrProbeRefused)
					assert.Contains(t, err.Error(), "BEGIN BATCH with IF MATCH", "an update's refusal keeps its advice")
					assert.Equal(t, 119, progress.Counts.NotAttempted)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, 1, progress.Counts.Failed)
			})
		})
	}
}

func refusals(from, to int) []mock.Option {
	var options []mock.Option
	for i := from; i < to; i++ {
		options = append(options, mock.WithEditRefusal(fmt.Sprintf("o%03d", i)))
	}
	return options
}

func TestTooManyFailuresEndTheJob(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, n int) {
		a := store(orders(400, "shipped"), refusals(1, 1+mutate.MaxFailures+1)...)
		m, targets := mustSelect(t, a, archive)

		progress, err := runJob(newJob(m, targets, editorOf(t, a), n, &fakeClock{}))

		require.ErrorIs(t, err, mutate.ErrTooManyFailures)
		assert.Equal(t, mutate.MaxFailures+1, progress.Counts.Failed)
		assert.Positive(t, progress.Counts.NotAttempted)
	})
}

func TestUnansweredWrites(t *testing.T) {
	tests := []struct {
		name    string
		unknown []string
		ended   bool
	}{
		{name: "two in a row do not end the job", unknown: []string{"o003", "o004"}},
		{name: "three apart do not either", unknown: []string{"o003", "o005", "o007"}},
		{name: "three in a row do", unknown: []string{"o003", "o004", "o005"}, ended: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var options []mock.Option
			for _, id := range tt.unknown {
				options = append(options, mock.WithEditUnknown(id))
			}
			a := store(orders(10, "shipped"), options...)
			m, targets := mustSelect(t, a, archive)
			j := newJob(m, targets, editorOf(t, a), 1, &fakeClock{})

			progress, err := runJob(j)

			if tt.ended {
				require.ErrorIs(t, err, mutate.ErrNoAnswer)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, len(tt.unknown), progress.Counts.Unknown)
			calls := map[string]int{}
			for _, id := range a.EditedIDs() {
				calls[id]++
			}
			for _, id := range tt.unknown {
				assert.Equal(t, 1, calls[id], "an unknown write is never sent again")
				assert.Equal(t, `"archived"`, storedField(t, a, id, "status"), "the mock applied it before losing the answer")
			}
			assert.False(t, j.Summary().Clean())
		})
	}
}

func TestAThrottledWriteIsRetriedAfterItsWait(t *testing.T) {
	eachWriterCount(t, func(t *testing.T, n int) {
		a := store(orders(40, "shipped"), mock.WithEditThrottle("o010", 2, 5*time.Second))
		m, targets := mustSelect(t, a, archive)
		clock := &fakeClock{}

		progress, err := runJob(newJob(m, targets, editorOf(t, a), n, clock))

		require.NoError(t, err)
		assert.Equal(t, mutate.Counts{Applied: 40}, progress.Counts)
		assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second}, clock.asked())
		assert.Equal(t, 2, progress.Throttles)
		assert.Equal(t, max(n-2, 1), progress.Writers, "each throttle takes a writer away")
		assert.Len(t, a.EditedIDs(), 42)
		assert.LessOrEqual(t, a.HighestConcurrentEdits(), n)
	})
}

func TestAWriteThrottledPastTheLimitEndsTheStepUnwritten(t *testing.T) {
	a := store(orders(3, "shipped"), mock.WithEditThrottle("o001", 100, time.Millisecond))
	m, targets := mustSelect(t, a, archive)
	j := newJob(m, targets, editorOf(t, a), 1, &fakeClock{})

	progress, err := runJob(j)

	var throttled *adapter.ThrottledError
	require.ErrorAs(t, err, &throttled)
	assert.Equal(t, mutate.Counts{Applied: 1, NotAttempted: 2}, progress.Counts, "o001 is left to the resume")
	assert.False(t, j.Done())
}

// gatedEditor holds the write of one item until released, and keeps what
// that write's context said when it returned.
type gatedEditor struct {
	adapter.ItemEditor
	gated   string
	entered chan struct{}
	release chan struct{}
	liveAt  error
}

func (e *gatedEditor) EditItem(ctx context.Context, container []string, key adapter.PartitionKey, op adapter.Operation) (adapter.OperationResult, error) {
	if op.ID != e.gated {
		return e.ItemEditor.EditItem(ctx, container, key, op)
	}
	close(e.entered)
	<-e.release
	result, err := e.ItemEditor.EditItem(ctx, container, key, op)
	e.liveAt = ctx.Err()
	return result, err
}

func TestAStopLetsTheWriteInFlightFinishAndStartsNoOther(t *testing.T) {
	a := store(orders(500, "shipped"))
	m, targets := mustSelect(t, a, archive)
	editor := &gatedEditor{ItemEditor: editorOf(t, a), gated: "o101", entered: make(chan struct{}), release: make(chan struct{})}
	j := newJob(m, targets, editor, 1, &fakeClock{})
	for range 2 { // the probe, then the first chunk
		_, err := j.ApplyChunk(context.Background())
		require.NoError(t, err)
	}

	ctx, stop := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() {
		_, err := j.ApplyChunk(ctx)
		ended <- err
	}()
	<-editor.entered
	stop()
	close(editor.release)

	require.ErrorIs(t, <-ended, context.Canceled)
	assert.NoError(t, editor.liveAt, "a write in flight is never cancelled by a stop")
	assert.Len(t, a.EditedIDs(), 102, "no write starts after the stop")
	summary := j.Summary()
	assert.Equal(t, mutate.Counts{Applied: 102, NotAttempted: 398}, summary.Counts)
	assert.False(t, summary.Clean())

	j.Resume()
	_, err := runJob(j)

	require.NoError(t, err)
	assert.ElementsMatch(t, ids(500), a.EditedIDs(), "each target written once across the stop")
	assert.Equal(t, mutate.Counts{Applied: 500}, j.Summary().Counts)
}

func TestRunningTheStatementAgainIsSafe(t *testing.T) {
	t.Run("a SET that falsifies the WHERE selects nothing the second time", func(t *testing.T) {
		a := store(orders(20, "shipped"))
		_, err := update(t, a, archive, 4)
		require.NoError(t, err)

		_, targets := mustSelect(t, a, archive)

		assert.Empty(t, targets.Items)
	})
	t.Run("one that does not writes the same bodies again", func(t *testing.T) {
		a := store(orders(20, "shipped"))
		_, err := update(t, a, flagAll, 4)
		require.NoError(t, err)
		first := bodies(t, a)

		j, err := update(t, a, flagAll, 4)

		require.NoError(t, err)
		assert.Equal(t, 20, j.Summary().Counts.Applied)
		assert.Equal(t, first, bodies(t, a))
	})
}

func bodies(t *testing.T, a *mock.Adapter) []string {
	t.Helper()
	var listed []string
	for _, item := range a.Items(ordersPath) {
		body, _, err := adapter.SplitSystemFields(item)
		require.NoError(t, err)
		listed = append(listed, string(body))
	}
	return listed
}

func TestANestedSetIsSentOnlyWhereItsParentIs(t *testing.T) {
	a := store([]json.RawMessage{
		json.RawMessage(`{"id":"with","customerId":"c01","status":"shipped","ship":{"city":"x"}}`),
		json.RawMessage(`{"id":"without","customerId":"c01","status":"shipped"}`),
		json.RawMessage(`{"id":"moved","customerId":"c01","status":"shipped","ship":{"city":"y"}}`),
	})
	m, targets := mustSelect(t, a, `UPDATE sales.orders o SET o.ship.region = "west" WHERE `+shipped)
	require.True(t, targets.WholeItems)
	assert.Equal(t, 1, targets.Unplaced())
	require.NoError(t, a.PutItem(ordersPath, json.RawMessage(`{"id":"moved","customerId":"c01","status":"shipped"}`)))
	editor := &recordingEditor{ItemEditor: editorOf(t, a)}

	progress, err := runJob(newJob(m, targets, editor, 1, &fakeClock{}))

	require.NoError(t, err, "no item fails, so the probe blames nothing")
	assert.Equal(t, mutate.Counts{Applied: 1, NoParent: 1, Changed: 1}, progress.Counts,
		"a parent removed since the selection is a change")
	for _, op := range editor.sent() {
		assert.Equal(t, `FROM o WHERE (o.status = "shipped") AND IS_DEFINED(o.ship)`, op.Condition)
		assert.NotEqual(t, "without", op.ID, "an item with nowhere to put the field is never sent")
	}
	assert.JSONEq(t, `{"city":"x","region":"west"}`, storedField(t, a, "with", "ship"))
}

func TestPreviewMarksASetWithNoParent(t *testing.T) {
	m := parse(t, `UPDATE sales.orders o SET o.ship.region = "w", o.lines[3] = 1, o.tags[0] = 2 WHERE true`)

	changes, err := mutate.Preview(json.RawMessage(`{"id":"o1","lines":[1]}`), m)

	require.NoError(t, err)
	assert.Equal(t, mutate.NoParent, changes[0].Kind)
	assert.Equal(t, mutate.NoParent, changes[1].Kind, "past an array's end a set appends, and again on a rerun")
	assert.Equal(t, mutate.NoParent, changes[2].Kind, "an index needs its array")
}
