package panes_test

import (
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const (
	paneWidth   = 28
	paneHeight  = 12
	statusWidth = 80
)

// TestMain pins a color profile. Without a TTY lipgloss renders everything
// unstyled, which erases the focus and selection differences these tests exist
// to observe.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

// plain drops the styling, so an assertion can match text a renderer split
// into separately styled runs — a cursor sitting on the first character of
// the editor's placeholder, say.
func plain(view string) string {
	return ansi.Strip(view)
}

func TestEditorAndResultsFillTheirFrames(t *testing.T) {
	panels := map[string]string{
		"editor":  panes.NewEditor().SetSize(paneWidth, paneHeight).View(),
		"results": panes.NewResults().SetSize(paneWidth, paneHeight).View(),
	}
	for name, view := range panels {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, paneWidth, lipgloss.Width(view))
			assert.Equal(t, paneHeight, lipgloss.Height(view))
		})
	}
}

func TestFocusChangesThePaneBorder(t *testing.T) {
	blurred := panes.NewEditor().SetSize(paneWidth, paneHeight).View()
	focused := panes.NewEditor().SetSize(paneWidth, paneHeight).Focus().View()

	assert.NotEqual(t, blurred, focused, "focus must be visible in the border")
	assert.Equal(t, lipgloss.Width(blurred), lipgloss.Width(focused))
}

func TestStatusBarShowsProfileScopeAndHelpHint(t *testing.T) {
	view := panes.NewStatusBar(theme.Icons(), "dev").
		SetWidth(statusWidth).
		SetScope([]string{"sales", "orders"}).
		View()

	assert.Contains(t, view, "dev")
	assert.Contains(t, view, "sales.orders")
	assert.Contains(t, view, "? help")
	assert.Equal(t, statusWidth, lipgloss.Width(view))
}

func TestStatusBarSaysWhenThereIsNoScope(t *testing.T) {
	view := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).View()

	assert.Contains(t, view, "no scope")
}

func TestStatusBarRetiresItsNotice(t *testing.T) {
	bar, expire := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).
		SetNotice("exported 10 rows")
	require.NotNil(t, expire, "a notice schedules its own retirement")
	require.Contains(t, plain(bar.View()), "exported 10 rows")

	bar, _ = bar.Update(panes.NoticeExpiredMsg{Notice: 1})

	assert.NotContains(t, plain(bar.View()), "exported 10 rows")
}

func TestStatusBarKeepsTheNoticeAnEarlierExpiryWouldWipe(t *testing.T) {
	bar, _ := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).SetNotice("exported 10 rows")
	bar, _ = bar.SetNotice("created sales.shipments")

	bar, _ = bar.Update(panes.NoticeExpiredMsg{Notice: 1})

	assert.Contains(t, plain(bar.View()), "created sales.shipments",
		"the first notice's expiry must not take the one that replaced it")
}

func TestStatusBarNeverOutgrowsItsWidth(t *testing.T) {
	widths := []struct {
		name  string
		width int
	}{
		{name: "far too narrow", width: 10},
		{name: "narrow", width: 30},
		{name: "minimum terminal", width: 80},
		{name: "wide", width: 200},
	}
	for _, tt := range widths {
		t.Run(tt.name, func(t *testing.T) {
			view := panes.NewStatusBar(theme.Icons(), "a-very-long-profile-name").
				SetWidth(tt.width).
				SetScope([]string{"database", "container"}).
				View()

			assert.LessOrEqual(t, lipgloss.Width(view), tt.width)
		})
	}
}

func TestStatusBarWaitsForAResultBeforeReportingStatistics(t *testing.T) {
	view := plain(panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).View())

	assert.Contains(t, view, "— rows")
	assert.Contains(t, view, "— RU")
	assert.Contains(t, view, "— elapsed")
}

func TestStatusBarReportsTheLoadedResultSet(t *testing.T) {
	bar, _ := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).SetProgress(panes.Progress{
		Stats:  adapter.Stats{RowCount: 120, RequestCharge: 4.25, Elapsed: 12 * time.Millisecond},
		More:   true,
		Loaded: true,
	})

	view := plain(bar.View())
	assert.Contains(t, view, "120 rows (+more)")
	assert.Contains(t, view, "4.25 RU")
	assert.Contains(t, view, "12ms")
}

func TestStatusBarDropsTheMoreHintOnTheLastPage(t *testing.T) {
	bar, _ := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).SetProgress(panes.Progress{
		Stats:  adapter.Stats{RowCount: 30},
		Loaded: true,
	})

	assert.NotContains(t, plain(bar.View()), "(+more)")
}

func TestStatusBarSpinsWhileAQueryRuns(t *testing.T) {
	bar, tick := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).
		SetProgress(panes.Progress{Running: true})

	assert.NotNil(t, tick, "a run that has just started drives the animation")
	assert.Contains(t, plain(bar.View()), theme.Icons().SpinnerFrames[0])
}

func TestStatusBarStopsSpinningOnceTheRunSettles(t *testing.T) {
	bar, tick := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).
		SetProgress(panes.Progress{Running: true})
	bar, again := bar.SetProgress(panes.Progress{Running: true})
	assert.Nil(t, again, "an animation already playing is not started twice")

	settled, _ := bar.SetProgress(panes.Progress{Loaded: true})
	_, stopped := settled.Update(tick())

	assert.Nil(t, stopped, "the last tick of a settled run ends the animation")
	assert.NotContains(t, plain(settled.View()), theme.Icons().SpinnerFrames[0])
}
