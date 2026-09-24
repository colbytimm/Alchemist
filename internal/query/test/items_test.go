package query_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

func rawItems(docs ...string) []json.RawMessage {
	items := make([]json.RawMessage, 0, len(docs))
	for _, doc := range docs {
		items = append(items, json.RawMessage(doc))
	}
	return items
}

func TestWholeItemsIsOnlyAStarredSelect(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{text: "SELECT * FROM c", want: true},
		{text: "select top 5 * from c where c.a = 1", want: true},
		{text: "SELECT * -- all\nFROM c", want: true},
		{text: "SELECT c.id FROM c", want: false},
		{text: "SELECT c.amount AS total FROM c", want: false},
		{text: "SELECT COUNT(1) FROM c", want: false},
		{text: "SELECT VALUE c FROM c", want: false},
		{text: "SELECT *", want: false},
		{text: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			leaf := query.Leaf{Query: adapterQuery(tt.text)}
			assert.Equal(t, tt.want, leaf.WholeItems())
		})
	}
}

func TestLeafItemsUndoesAUnionTag(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM sales.orders, sales.archive AS c")
	require.NoError(t, err)

	got := plan.LeafItems(rawItems(
		`{"_container":"sales.orders","id":"1"}`,
		`{"_container":"sales.archive","id":"2","archivedAt":"x"}`,
		`{"_container":"sales.orders"}`,
		`{"_container":"hr.people","id":"3"}`,
		`"not an object"`,
	))

	require.Len(t, got, 2)
	assert.Equal(t, rawItems(`{"id":"1"}`, `{}`), got[0])
	assert.Equal(t, rawItems(`{"id":"2","archivedAt":"x"}`), got[1])
}

func TestLeafItemsUndoesAJoinPair(t *testing.T) {
	plan, err := query.BuildPlan("SELECT o.id, cu.name FROM sales.orders o JOIN sales.customers cu ON o.customerId = cu.id")
	require.NoError(t, err)

	got := plan.LeafItems(rawItems(`{"o":{"id":"1"},"cu":{"name":"Ann"}}`, `{"o":{"id":"2"}}`, `[]`))

	require.Len(t, got, 2)
	assert.Equal(t, rawItems(`{"id":"1"}`, `{"id":"2"}`), got[0])
	assert.Equal(t, rawItems(`{"name":"Ann"}`), got[1])
}

func TestLeafItemsHandsAPassThroughPageToItsLeaf(t *testing.T) {
	plan, err := query.BuildPlan("SELECT * FROM c")
	require.NoError(t, err)

	items := rawItems(`{"id":"1"}`)
	assert.Equal(t, [][]json.RawMessage{items}, plan.LeafItems(items))
	assert.Empty(t, query.Plan{}.LeafItems(items), "a plan with no leaves owns nothing")
}

func adapterQuery(text string) adapter.Query {
	return adapter.Query{Text: text}
}
