package panes_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	resultsWidth  = 60
	resultsHeight = 10
	// narrowResultsWidth has room for two of the fixture's three columns.
	narrowResultsWidth = 15
)

var firstPage = adapter.Page{
	Columns: []string{"id", "region", "note"},
	Rows: [][]string{
		{"a-1", "west", "first"},
		{"a-2", "east", "second"},
	},
	Raw: []json.RawMessage{
		json.RawMessage(`{"id":"a-1"}`),
		json.RawMessage(`{"id":"a-2"}`),
	},
}

var secondPage = adapter.Page{
	Columns: []string{"id", "region", "note", "extra"},
	Rows:    [][]string{{"a-3", "north", "third", "late"}},
	Raw:     []json.RawMessage{json.RawMessage(`{"id":"a-3"}`)},
}

func newTable() panes.Results {
	return panes.NewResults().SetSize(resultsWidth, resultsHeight).Load(firstPage)
}

func TestResultsRendersItsHeaderAndRows(t *testing.T) {
	view := plain(newTable().View())

	for _, want := range []string{"id", "region", "note", "a-1", "east", "second"} {
		assert.Contains(t, view, want)
	}
}

func TestResultsSaysWhenNothingHasRun(t *testing.T) {
	view := panes.NewResults().SetSize(resultsWidth, resultsHeight).View()

	assert.Contains(t, plain(view), "no results yet")
}

func TestResultsCapsAWideColumn(t *testing.T) {
	wide := adapter.Page{Columns: []string{"note"}, Rows: [][]string{{strings.Repeat("x", 80)}}}

	view := plain(panes.NewResults().SetSize(resultsWidth, resultsHeight).Load(wide).View())

	assert.Contains(t, view, "…", "the value is truncated, not wrapped")
	assert.NotContains(t, view, strings.Repeat("x", 30))
}

func TestResultsScrollsColumnsSideways(t *testing.T) {
	narrow := panes.NewResults().SetSize(narrowResultsWidth, resultsHeight).Load(firstPage)
	require.NotContains(t, plain(narrow.View()), "note", "the last column does not fit")

	scrolled := narrow.ScrollRight().ScrollRight()

	assert.Contains(t, plain(scrolled.View()), "note")
	assert.NotContains(t, plain(scrolled.View()), "region")
	assert.Contains(t, plain(scrolled.ScrollLeft().ScrollLeft().View()), "region", "and back again")
}

func TestResultsMarksTheRowUnderTheCursor(t *testing.T) {
	table := newTable()

	assert.NotEqual(t, table.View(), table.CursorDown().View(), "the cursor has to be visible")
}

func TestResultsCursorStopsAtTheEndsOfTheRows(t *testing.T) {
	table := newTable()
	assert.Equal(t, table.View(), table.CursorUp().View(), "the cursor cannot move above the first row")

	last := table.CursorDown()
	require.True(t, last.AtLastRow())
	assert.Equal(t, last.View(), last.CursorDown().View(), "the cursor cannot move past the last row")
}

func TestResultsAppendKeepsTheColumnOrderAndAddsLaterColumnsAtTheEnd(t *testing.T) {
	view := plain(newTable().Append(secondPage).View())

	assert.Contains(t, view, "a-1", "the first page stays")
	assert.Contains(t, view, "a-3")
	assert.Less(t, strings.Index(view, "region"), strings.Index(view, "extra"),
		"a column a later page introduced belongs after the locked ones")
}

func TestResultsRendersRowsFetchedBeforeALaterColumnExisted(t *testing.T) {
	view := plain(newTable().Append(secondPage).View())

	assert.Contains(t, view, "extra", "the column the later page introduced joins the header")
	assert.Contains(t, view, "late", "the row that carries it shows its value")
	assert.Contains(t, view, "first", "and the shorter rows still render under the columns they have")
}

func TestResultsSaysWhenAQueryReturnedNoRows(t *testing.T) {
	view := plain(panes.NewResults().SetSize(resultsWidth, resultsHeight).Load(adapter.Page{}).View())

	assert.Contains(t, view, "no rows")
	assert.NotContains(t, view, "no results yet", "a query that ran is not a query that has not")
}

func TestResultsFailureLeavesFetchedRowsOnScreen(t *testing.T) {
	view := plain(newTable().Fail(errors.New("page 2 unreachable")).View())

	assert.Contains(t, view, "page 2 unreachable")
	assert.Contains(t, view, "a-1", "a page that never arrived says nothing about the ones that did")
}

func TestResultsKeepsRowsVisibleUnderATallFailure(t *testing.T) {
	tall := errors.New(strings.Repeat("the service rejected the request. ", 40))

	view := plain(newTable().Fail(tall).View())

	assert.Contains(t, view, "the service rejected the request.")
	assert.Contains(t, view, "a-1", "a failure longer than the pane must not push the rows out of it")
}

func TestResultsFailureStandsAloneWhenNothingLoaded(t *testing.T) {
	view := plain(panes.NewResults().SetSize(resultsWidth, resultsHeight).
		Fail(errors.New("syntax error near FROM")).View())

	assert.Contains(t, view, "syntax error near FROM")
	assert.NotContains(t, view, "no results yet")
}

func TestResultsClearEmptiesThePane(t *testing.T) {
	view := plain(newTable().Fail(errors.New("gone")).Clear().View())

	assert.Contains(t, view, "no results yet")
	assert.NotContains(t, view, "gone")
	assert.NotContains(t, view, "a-1")
}

func TestResultsSelectedDocumentFollowsTheCursor(t *testing.T) {
	document, ok := newTable().CursorDown().SelectedDocument()

	require.True(t, ok)
	assert.JSONEq(t, `{"id":"a-2"}`, string(document))
}

func TestResultsHasNoDocumentWithoutRows(t *testing.T) {
	_, ok := panes.NewResults().SelectedDocument()

	assert.False(t, ok)
}
