package panes_test

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

const statusWidth = 80

// TestMain pins a color profile. Without a TTY lipgloss renders everything
// unstyled, which erases the focus and selection differences these tests exist
// to observe.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func TestEditorAndResultsFillTheirFrames(t *testing.T) {
	views := map[string]string{
		"editor":  panes.NewEditor().SetSize(paneWidth, paneHeight).View(),
		"results": panes.NewResults().SetSize(paneWidth, paneHeight).View(),
	}
	for name, view := range views {
		assert.Equal(t, paneWidth, lipgloss.Width(view), name)
		assert.Equal(t, paneHeight, lipgloss.Height(view), name)
	}
}

func TestFocusChangesThePaneBorder(t *testing.T) {
	blurred := panes.NewEditor().SetSize(paneWidth, paneHeight).View()
	focused := panes.NewEditor().SetSize(paneWidth, paneHeight).Focus().View()

	assert.NotEqual(t, blurred, focused, "focus must be visible in the border")
	assert.Equal(t, lipgloss.Width(blurred), lipgloss.Width(focused))
}

func TestStatusBarShowsProfileScopeAndHelpHint(t *testing.T) {
	bar := panes.NewStatusBar(theme.Icons(), "dev").
		SetWidth(statusWidth).
		SetScope([]string{"sales", "orders"})

	view := bar.View()

	assert.Contains(t, view, "dev")
	assert.Contains(t, view, "sales.orders")
	assert.Contains(t, view, "? help")
	assert.Equal(t, statusWidth, lipgloss.Width(view))
}

func TestStatusBarSaysWhenThereIsNoScope(t *testing.T) {
	view := panes.NewStatusBar(theme.Icons(), "dev").SetWidth(statusWidth).View()

	assert.Contains(t, view, "no scope")
}

func TestStatusBarNeverOutgrowsItsWidth(t *testing.T) {
	for _, width := range []int{10, 30, 80, 200} {
		bar := panes.NewStatusBar(theme.Icons(), "a-very-long-profile-name").
			SetWidth(width).
			SetScope([]string{"database", "container"})

		assert.LessOrEqual(t, lipgloss.Width(bar.View()), width, "width %d", width)
	}
}
