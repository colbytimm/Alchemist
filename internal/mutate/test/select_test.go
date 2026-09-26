package mutate_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/mutate"
)

func TestASelectionPagesToTheEndThenReadsAPreview(t *testing.T) {
	a := store(append(orders(2500, "shipped"), order(2500, "open")))

	_, targets := mustSelect(t, a, archive)

	assert.Len(t, targets.Items, 2500)
	assert.Equal(t, 4*2.5, targets.RequestCharge, "three pages of identities and one of the preview")
	require.Len(t, targets.Preview, 3)
	assert.JSONEq(t, string(order(0, "shipped")), string(stripSystemFields(t, targets.Preview[0])))
	assert.False(t, targets.WholeItems)
	assert.False(t, targets.SelectedAt.IsZero())
	first := targets.Items[0]
	assert.Equal(t, "o000", first.ID)
	assert.Equal(t, `"c00"`, string(first.Key[0]))
	assert.NotEmpty(t, first.Version)
}

func stripSystemFields(t *testing.T, item json.RawMessage) json.RawMessage {
	t.Helper()
	body, _, err := adapter.SplitSystemFields(item)
	require.NoError(t, err)
	return body
}

func TestASelectionPastItsLimitRefusesEverything(t *testing.T) {
	tests := []struct {
		name    string
		matches int
		limit   int
		refused bool
	}{
		{name: "at the limit", matches: 5, limit: 5},
		{name: "one past it", matches: 6, limit: 5, refused: true},
		{name: "past the default", matches: mutate.DefaultMaxTargets + 1, refused: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := store(orders(tt.matches, "shipped"))

			targets, err := selectTargets(t, a, parse(t, archive), tt.limit)

			if !tt.refused {
				require.NoError(t, err)
				assert.Len(t, targets.Items, tt.matches)
				return
			}
			require.ErrorIs(t, err, mutate.ErrTooManyTargets)
			assert.Empty(t, targets.Items, "no truncated list")
			assert.Positive(t, targets.RequestCharge, "the charge of the read is still known")
		})
	}
}

func TestNoMatchIsAnEmptySelectionWithNoPreview(t *testing.T) {
	a := store(orders(3, "open"))

	_, targets := mustSelect(t, a, archive)

	assert.Empty(t, targets.Items)
	assert.Empty(t, targets.Preview)
	assert.Equal(t, 2.5, targets.RequestCharge, "one page, and no preview read")
}

func TestAnItemWithNoKeyIsKeptAndMarked(t *testing.T) {
	a := store([]json.RawMessage{order(0, "shipped"), json.RawMessage(`{"id":"keyless","status":"shipped"}`)})

	_, targets := mustSelect(t, a, archive)

	require.Len(t, targets.Items, 2)
	assert.Nil(t, targets.Items[1].Key)
	assert.Equal(t, 1, targets.Keyless())
}

func TestAnUnsetReadsWholeItemsAndDropsThoseItCannotChange(t *testing.T) {
	a := store([]json.RawMessage{
		json.RawMessage(`{"id":"with","customerId":"c01","status":"shipped","note":"x"}`),
		json.RawMessage(`{"id":"without","customerId":"c01","status":"shipped"}`),
	})

	_, targets := mustSelect(t, a, dropNote)

	assert.True(t, targets.WholeItems)
	require.Len(t, targets.Items, 1)
	assert.Equal(t, "with", targets.Items[0].ID)
	assert.Equal(t, 1, targets.Unaffected)
	assert.Len(t, targets.Preview, 1, "whole items are their own preview")
}

func TestAnUnsetBesideASetKeepsEveryMatch(t *testing.T) {
	a := store([]json.RawMessage{
		json.RawMessage(`{"id":"with","customerId":"c01","status":"shipped","note":"x"}`),
		json.RawMessage(`{"id":"without","customerId":"c01","status":"shipped"}`),
	})

	_, targets := mustSelect(t, a, `UPDATE sales.orders o SET o.done = true UNSET o.note WHERE `+shipped)

	require.Len(t, targets.Items, 2)
	assert.Zero(t, targets.Items[0].Absent)
	assert.Equal(t, uint16(1), targets.Items[1].Absent)
	assert.Zero(t, targets.Unaffected)
}
