package panes_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// The smallest terminal the application supports.
const (
	minimumWidth  = 80
	minimumHeight = 24
)

var (
	confirmKey = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "delete"))
	savedHints = []key.Binding{
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	}
)

func savedQuery(name, text string, scope ...string) saved.Query {
	return saved.Query{Name: name, Text: text, Scope: scope, Saved: loadedAt.Add(-2 * time.Hour)}
}

func fixtureSaved() saved.Listing {
	return saved.Listing{Queries: []saved.Query{
		savedQuery("every-customer", "SELECT * FROM sales.customers c"),
		savedQuery("late-shipments", "SELECT c.id\nFROM c\nWHERE c.late", "sales", "orders"),
		savedQuery("open orders", "SELECT c.id, c.total\nFROM c", "sales", "orders"),
	}}
}

func newSaved(width, height int, listing saved.Listing) panes.Saved {
	return panes.NewSaved(theme.Icons(), savedHints, confirmKey).
		SetSize(width, height).
		SetAccount("prod").
		SetListing("prod", listing, loadedAt)
}

func TestSavedRendersNameScopeFirstLineAndAge(t *testing.T) {
	view := plain(newSaved(minimumWidth, minimumHeight, fixtureSaved()).View())

	assert.Contains(t, view, " Saved queries · prod ")
	row := lineWith(t, view, "late-shipments")
	assert.Contains(t, row, "sales.orders")
	assert.Contains(t, row, "SELECT c.id")
	assert.Contains(t, row, "2h ago")
}

func TestSavedPreviewsTheWholeSelectedQuery(t *testing.T) {
	s := newSaved(minimumWidth, minimumHeight, fixtureSaved()).CursorDown()

	view := plain(s.View())

	assert.Contains(t, view, "WHERE c.late")
}

func TestSavedPreviewGivesWayOnAShortTerminal(t *testing.T) {
	s := newSaved(minimumWidth, 10, fixtureSaved()).CursorDown()

	view := plain(s.View())

	assert.NotContains(t, view, "WHERE c.late")
	assert.Contains(t, view, "late-shipments")
	assert.Contains(t, view, "/ filter", "the hint line stays")
}

func TestSavedFillsTheMinimumTerminalWithItsHintLineLast(t *testing.T) {
	lines := strings.Split(plain(newSaved(minimumWidth, minimumHeight, fixtureSaved()).View()), "\n")

	require.Len(t, lines, minimumHeight)
	assert.Contains(t, lines[len(lines)-2], "/ filter")
	for _, line := range lines {
		assert.LessOrEqual(t, lipgloss.Width(line), minimumWidth)
	}
}

func TestSavedCountsSkippedFiles(t *testing.T) {
	listing := fixtureSaved()
	listing.Skipped = []saved.Skipped{{File: "a.sql", Err: errors.New("bad")}}

	assert.Contains(t, plain(newSaved(minimumWidth, minimumHeight, listing).View()), "1 file skipped, see the log")
	assert.NotContains(t, plain(newSaved(minimumWidth, minimumHeight, fixtureSaved()).View()), "skipped")
}

func TestSavedSaysWhenTheAccountHasNothingSaved(t *testing.T) {
	view := plain(newSaved(minimumWidth, minimumHeight, saved.Listing{}).View())

	assert.Contains(t, view, "nothing saved for prod yet")
}

func TestSavedFilterNarrowsByNameTextAndScope(t *testing.T) {
	for needle, want := range map[string]string{"open": "open orders", "customers": "every-customer", "sales.orders": "late-shipments"} {
		s := newSaved(minimumWidth, minimumHeight, fixtureSaved()).StartFilter()
		s, _ = s.Update(typed(needle))

		got, ok := s.Selected()

		require.True(t, ok, needle)
		assert.Equal(t, want, got.Name, "filtering by %q", needle)
	}
}

func TestSavedKeepsTheCursorOnTheSameNameAcrossAReload(t *testing.T) {
	s := newSaved(minimumWidth, minimumHeight, fixtureSaved()).CursorDown().CursorDown()
	listing := fixtureSaved()
	listing.Queries = append([]saved.Query{savedQuery("archive", "SELECT 1")}, listing.Queries...)

	s = s.SetListing("prod", listing, loadedAt)

	got, _ := s.Selected()
	assert.Equal(t, "open orders", got.Name)
}

func TestSavedMovesToANeighborWhenTheSelectedQueryIsGone(t *testing.T) {
	s := newSaved(minimumWidth, minimumHeight, fixtureSaved()).CursorDown().CursorDown()
	listing := fixtureSaved()
	listing.Queries = listing.Queries[:2]

	s = s.SetListing("prod", listing, loadedAt)

	got, ok := s.Selected()
	require.True(t, ok)
	assert.Equal(t, "late-shipments", got.Name)
}

func TestSavedAsksBeforeADelete(t *testing.T) {
	s := newSaved(minimumWidth, minimumHeight, fixtureSaved()).AskDelete()

	assert.True(t, s.Deleting())
	assert.Contains(t, plain(s.View()), `delete "every-customer"?  y delete   any other key keeps it`)
	assert.False(t, s.CancelDelete().Deleting())
}

func TestSavedAsksNothingWithNothingSelected(t *testing.T) {
	assert.False(t, newSaved(minimumWidth, minimumHeight, saved.Listing{}).AskDelete().Deleting())
}

func TestSavedFailureShowsInPlaceOfTheList(t *testing.T) {
	view := plain(newSaved(minimumWidth, minimumHeight, fixtureSaved()).Fail(errors.New("permission denied")).View())

	assert.Contains(t, view, "permission denied")
	assert.NotContains(t, view, "open orders")
}

func newSavePrompt() panes.SavePrompt {
	return panes.NewSavePrompt(nil).SetSize(minimumWidth, minimumHeight)
}

func draft() panes.SaveDraft {
	return panes.SaveDraft{Account: "prod", Text: "SELECT c.id\nFROM c", Scope: []string{"sales", "orders"}}
}

func TestSavePromptShowsTheAccountScopeAndFirstLine(t *testing.T) {
	view := plain(newSavePrompt().OpenSave(draft()).View())

	assert.Contains(t, view, " Save query ")
	assert.Contains(t, view, "account  prod")
	assert.Contains(t, view, "scope    sales.orders")
	assert.Contains(t, view, "query    SELECT c.id")
	assert.NotContains(t, view, "FROM c")
}

func TestSavePromptSaysWhenNoScopeIsSaved(t *testing.T) {
	d := draft()
	d.Scope = nil

	assert.Contains(t, plain(newSavePrompt().OpenSave(d).View()), "scope    none")
}

func TestSavePromptReadsATrailingMarkAsReplace(t *testing.T) {
	p, _ := newSavePrompt().OpenSave(draft()).Update(typed(" open orders ! "))

	assert.Equal(t, panes.SaveTarget{Account: "prod", Name: "open orders", Replace: true}, p.Target())
}

func TestSavePromptOpensEmptyUnlessANameIsSeeded(t *testing.T) {
	d := draft()
	d.Name = "open orders"

	assert.Equal(t, "", newSavePrompt().OpenSave(draft()).Target().Name)
	assert.Equal(t, panes.SaveTarget{Account: "prod", Name: "open orders"}, newSavePrompt().OpenSave(d).Target())
}

func TestRenamePromptKeepsTheMarkInTheName(t *testing.T) {
	d := draft()
	d.Name = "open orders"
	p, _ := newSavePrompt().OpenRename(d).Update(typed("!"))

	assert.Equal(t, panes.SaveTarget{Account: "prod", Name: "open orders!"}, p.Target())
	assert.True(t, p.Renaming())
	view := plain(p.View())
	assert.Contains(t, view, " Rename query ")
	assert.NotContains(t, view, "scope ")
	assert.NotContains(t, view, "End the name with !")
}

func TestSavePromptFailureKeepsTheName(t *testing.T) {
	p, _ := newSavePrompt().OpenSave(draft()).Update(typed("orders"))

	p = p.StartSaving().Fail(errors.New("already saved"))

	assert.False(t, p.Saving())
	assert.Equal(t, "orders", p.Target().Name)
	assert.Contains(t, plain(p.View()), "already saved")
}

// lineWith is the line of view holding text.
func lineWith(t *testing.T, view, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	require.Fail(t, "not on screen", text)
	return ""
}
