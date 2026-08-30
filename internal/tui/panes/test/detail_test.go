package panes_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func newDetail(document string) panes.Detail {
	return panes.NewDetail().SetSize(paneWidth, paneHeight).SetDocument(json.RawMessage(document))
}

func TestDetailIndentsTheDocument(t *testing.T) {
	view := plain(newDetail(`{"id":"a-1","amount":2}`).View())

	assert.Contains(t, view, `"id": "a-1"`)
	assert.Contains(t, view, `"amount": 2`)
}

func TestDetailShowsContentItCannotIndentVerbatim(t *testing.T) {
	view := plain(newDetail("not json").View())

	assert.Contains(t, view, "not json")
}

func TestDetailScrollsADocumentTallerThanThePane(t *testing.T) {
	detail := newDetail("[" + strings.Repeat("1,", 40) + "1]")

	require.NotEqual(t, detail.View(), detail.ScrollDown().View())
	assert.Equal(t, detail.View(), detail.ScrollDown().ScrollUp().View())
	assert.Equal(t, detail.View(), detail.ScrollUp().View(), "the first line is as far up as it goes")
}

func TestDetailStopsScrollingAtTheLastLine(t *testing.T) {
	detail := newDetail(`{"id":"a-1"}`)

	assert.Equal(t, detail.View(), detail.ScrollDown().View(), "a document that fits does not scroll")
}
