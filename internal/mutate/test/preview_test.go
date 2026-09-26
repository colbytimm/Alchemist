package mutate_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/mutate"
)

func TestPreviewWorksOutEachChange(t *testing.T) {
	m := parse(t, `UPDATE sales.orders o SET o.status = "archived", o.archivedAt = "2026-01-01", o.total = 5.0, o.lines[1].qty = 2 UNSET o.note, o.tmp WHERE true`)
	item := json.RawMessage(`{"id":"o1","status":"shipped","total":5,"note":"x","lines":[{"qty":1},{"qty":9}]}`)

	changes, err := mutate.Preview(item, m)

	require.NoError(t, err)
	assert.Equal(t, []mutate.FieldChange{
		{Kind: mutate.Changed, Path: "/status", Before: json.RawMessage(`"shipped"`), After: json.RawMessage(`"archived"`)},
		{Kind: mutate.Added, Path: "/archivedAt", After: json.RawMessage(`"2026-01-01"`)},
		{Kind: mutate.Unchanged, Path: "/total", Before: json.RawMessage(`5`), After: json.RawMessage(`5.0`)},
		{Kind: mutate.Changed, Path: "/lines/1/qty", Before: json.RawMessage(`9`), After: json.RawMessage(`2`)},
		{Kind: mutate.Removed, Path: "/note", Before: json.RawMessage(`"x"`)},
	}, changes, "an UNSET of a path the item lacks touches nothing")
}

func TestPreviewRefusesWhatIsNotAnItem(t *testing.T) {
	_, err := mutate.Preview(json.RawMessage(`[1]`), parse(t, archive))

	require.Error(t, err)
}

func TestConfirmation(t *testing.T) {
	assert.Equal(t, "orders", mutate.Confirmation(parse(t, archive), 412))
	assert.Equal(t, "orders 60", mutate.Confirmation(parse(t, `UPDATE sales.orders o SET o.x = 1 WHERE true`), 60))
}

func TestChangesNameEveryOperation(t *testing.T) {
	m := parse(t, `UPDATE sales.orders o SET o.status = "archived" UNSET o.tmp WHERE true`)

	assert.Equal(t, []string{`set /status "archived"`, "remove /tmp"}, mutate.Changes(m))
}
