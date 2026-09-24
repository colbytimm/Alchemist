package panes_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// historyWidth has room for a full row of the fixture below.
const historyWidth = 80

// loadedAt is the moment the fixture entries are aged against.
var loadedAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

var (
	orders = []string{"sales", "orders"}
	events = []string{"telemetry", "events"}
)

var historyHints = []key.Binding{
	key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "recall")),
}

func ran(age time.Duration, scope []string, query string) history.Entry {
	return history.Entry{
		Time:          loadedAt.Add(-age),
		Profile:       "dev",
		Scope:         scope,
		Query:         query,
		OK:            true,
		Rows:          10,
		RequestCharge: 2.5,
		ElapsedMillis: 5,
	}
}

func failedRun(age time.Duration, scope []string, query string) history.Entry {
	entry := ran(age, scope, query)
	entry.OK = false
	entry.Rows, entry.RequestCharge = 0, 0
	entry.Error = "syntax error"
	return entry
}

// fixtureLog is what a store serves: newest first.
func fixtureLog() []history.Entry {
	return []history.Entry{
		ran(30*time.Second, orders, "SELECT * FROM c WHERE c.amount > 100"),
		failedRun(5*time.Minute, events, "SELECT TOP 5 * FRM c"),
	}
}

func newHistory(entries ...history.Entry) panes.History {
	return panes.NewHistory(theme.Icons(), historyHints).
		SetSize(historyWidth, paneHeight).
		SetEntries("dev", entries, loadedAt)
}

func typeFilter(pane panes.History, text string) panes.History {
	pane = pane.StartFilter()
	for _, r := range text {
		pane, _ = pane.Update(typed(string(r)))
	}
	return pane
}

func TestHistoryRendersAgeOutcomeScopeQueryAndCharge(t *testing.T) {
	view := plain(newHistory(fixtureLog()...).View())

	for _, want := range []string{
		"just now", theme.Icons().Success, "sales.orders", "SELECT * FROM c WHERE c.amount > 100", "2.50 RU",
		"5m ago", theme.Icons().Failure, "telemetry.events", "SELECT TOP 5 * FRM c",
	} {
		assert.Contains(t, view, want)
	}
}

func TestHistoryShowsNoChargeForAFailedRun(t *testing.T) {
	view := plain(newHistory(failedRun(time.Minute, orders, "SELECT 1")).View())

	assert.Contains(t, view, "—")
	assert.NotContains(t, view, "0.00 RU")
}

func TestHistoryAgesEntriesAgainstTheMomentTheyLoaded(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "seconds are just now", age: 45 * time.Second, want: "just now"},
		{name: "minutes", age: 12 * time.Minute, want: "12m ago"},
		{name: "hours", age: 3 * time.Hour, want: "3h ago"},
		{name: "days", age: 49 * time.Hour, want: "2d ago"},
		{name: "a clock that ran backwards is just now", age: -time.Hour, want: "just now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, plain(newHistory(ran(tt.age, orders, "SELECT 1")).View()), tt.want)
		})
	}
}

func TestHistoryDatesARunOlderThanAMonth(t *testing.T) {
	entry := ran(45*24*time.Hour, orders, "SELECT 1")

	view := plain(newHistory(entry).View())

	assert.Contains(t, view, entry.Time.Local().Format(time.DateOnly), "in the local zone, like a person would say it")
	assert.NotContains(t, view, "ago")
}

func TestHistoryShowsOnlyTheFirstLineOfAQuery(t *testing.T) {
	view := plain(newHistory(ran(time.Minute, orders, "  SELECT *\nFROM c\nWHERE c.id = 1")).View())

	assert.Contains(t, view, "SELECT *")
	assert.NotContains(t, view, "FROM c")
}

func TestHistoryShowsItsKeysInAHintLine(t *testing.T) {
	view := plain(newHistory(fixtureLog()...).View())

	assert.Contains(t, view, "/ filter")
	assert.Contains(t, view, "enter recall")
}

func TestHistorySaysWhenNothingHasBeenRecorded(t *testing.T) {
	view := plain(newHistory().View())

	assert.Contains(t, view, "no queries recorded yet")
}

func TestHistoryFillsItsFrame(t *testing.T) {
	view := newHistory(fixtureLog()...).View()

	assert.Equal(t, historyWidth, lipgloss.Width(view))
	assert.Equal(t, paneHeight, lipgloss.Height(view))
}

func TestHistoryFilterNarrowsByQueryOrScope(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		want    string
		notWant string
	}{
		{name: "query text", filter: "amount", want: "c.amount", notWant: "TOP 5"},
		{name: "scope", filter: "telemetry", want: "TOP 5", notWant: "c.amount"},
		{name: "case aside", filter: "select top", want: "TOP 5", notWant: "c.amount"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := plain(typeFilter(newHistory(fixtureLog()...), tt.filter).View())

			assert.Contains(t, view, "/ "+tt.filter, "the filter line shows what was typed")
			assert.Contains(t, view, tt.want)
			assert.NotContains(t, view, tt.notWant)
		})
	}
}

func TestHistorySaysWhenNothingMatchesTheFilter(t *testing.T) {
	view := plain(typeFilter(newHistory(fixtureLog()...), "nowhere").View())

	assert.Contains(t, view, "nothing matches")
}

func TestHistoryClearFilterRestoresTheList(t *testing.T) {
	pane := typeFilter(newHistory(fixtureLog()...), "telemetry")
	require.NotContains(t, plain(pane.View()), "c.amount")

	cleared := pane.ClearFilter()

	assert.Contains(t, plain(cleared.View()), "c.amount")
	assert.False(t, cleared.Filtering())
}

func TestHistoryFilteringReportsWhereTypingGoes(t *testing.T) {
	pane := newHistory(fixtureLog()...)

	assert.False(t, pane.Filtering())
	assert.True(t, pane.StartFilter().Filtering())
}

func TestHistorySelectedFollowsTheCursor(t *testing.T) {
	pane := newHistory(fixtureLog()...)

	first, ok := pane.Selected()
	require.True(t, ok)
	assert.Equal(t, "SELECT * FROM c WHERE c.amount > 100", first.Query, "the newest entry is selected first")

	second, ok := pane.CursorDown().Selected()
	require.True(t, ok)
	assert.Equal(t, "SELECT TOP 5 * FRM c", second.Query)
}

func TestHistoryCursorStopsAtTheEnds(t *testing.T) {
	pane := newHistory(fixtureLog()...)

	assert.Equal(t, pane.View(), pane.CursorUp().View(), "the cursor cannot move above the first row")
	last := pane.CursorDown()
	assert.Equal(t, last.View(), last.CursorDown().View(), "the cursor cannot move past the last row")
}

func TestHistoryScrollsToKeepTheCursorOnScreen(t *testing.T) {
	entries := make([]history.Entry, 0, 2*paneHeight)
	for i := range cap(entries) {
		entries = append(entries, ran(time.Minute, orders, fmt.Sprintf("SELECT %d", i)))
	}
	pane := newHistory(entries...)
	require.NotContains(t, plain(pane.View()), "SELECT 23", "the last entry starts past the fold")

	for range len(entries) {
		pane = pane.CursorDown()
	}

	view := plain(pane.View())
	assert.Contains(t, view, "SELECT 23")
	assert.NotContains(t, view, "SELECT 0 ", "the first entry scrolled off")
}

func TestHistoryKeepsARowWithAWideChargeInsideTheFrame(t *testing.T) {
	entry := ran(time.Minute, orders, "SELECT * FROM c")
	entry.RequestCharge = 123456.78

	view := plain(newHistory(entry).View())

	assert.Contains(t, view, "123456.78 RU")
	assert.Equal(t, historyWidth, lipgloss.Width(view))
}

func TestHistoryMarksTheRowUnderTheCursor(t *testing.T) {
	pane := newHistory(fixtureLog()...)

	assert.NotEqual(t, pane.View(), pane.CursorDown().View(), "the cursor has to be visible")
}

func TestHistorySelectedIsTheFirstMatchOnceFiltered(t *testing.T) {
	pane := newHistory(fixtureLog()...).CursorDown()

	selected, ok := typeFilter(pane, "amount").Selected()

	require.True(t, ok)
	assert.Equal(t, "SELECT * FROM c WHERE c.amount > 100", selected.Query,
		"the row the cursor was on is filtered out, so it returns to the top")
}

func TestHistoryHasNoSelectionWithoutEntries(t *testing.T) {
	_, ok := newHistory().Selected()

	assert.False(t, ok)
}

func TestHistoryFailureShowsInPlaceOfTheEntries(t *testing.T) {
	view := plain(newHistory(fixtureLog()...).Fail(errors.New("history.jsonl: permission denied")).View())

	assert.Contains(t, view, "permission denied")
	assert.NotContains(t, view, "c.amount")
}

func TestHistorySetEntriesStartsOver(t *testing.T) {
	pane := typeFilter(newHistory(fixtureLog()...).CursorDown(), "telemetry")

	fresh := pane.SetEntries("dev", fixtureLog(), loadedAt)

	assert.False(t, fresh.Filtering())
	selected, ok := fresh.Selected()
	require.True(t, ok)
	assert.Equal(t, "SELECT * FROM c WHERE c.amount > 100", selected.Query)
	assert.Contains(t, plain(fresh.View()), "c.amount")
}
