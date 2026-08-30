package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The pane titles as they appear inside the top border.
const (
	catalogTitle = " Catalog "
	editorTitle  = " Editor "
	resultsTitle = " Results "
)

func TestViewShowsEveryPaneAndTheStatusBar(t *testing.T) {
	view := loaded(t, newCatalog(t)).View()

	for _, want := range []string{catalogTitle, editorTitle, resultsTitle} {
		assert.Contains(t, view, want)
	}
	assert.Contains(t, view, mock.Name, "status bar should name the profile")
	assert.Contains(t, view, "no scope", "status bar should say when no container is selected")
	assert.Contains(t, view, "? help")
}

func TestTabCyclesFocusThroughEveryPane(t *testing.T) {
	m := loaded(t, newCatalog(t))
	assertCatalogHasFocus(t, m, true)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assertCatalogHasFocus(t, m, false)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assertCatalogHasFocus(t, m, false)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assertCatalogHasFocus(t, m, true)
}

func TestShiftTabCyclesFocusBackwards(t *testing.T) {
	m := loaded(t, newCatalog(t))

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assertCatalogHasFocus(t, m, false)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assertCatalogHasFocus(t, m, false)

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assertCatalogHasFocus(t, m, true)
}

func TestF2FocusesTheEditor(t *testing.T) {
	m := loaded(t, newCatalog(t))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyF2})

	assertCatalogHasFocus(t, m, false)
}

// assertCatalogHasFocus checks focus by whether the catalog cursor still moves,
// which only the focused pane's keys can do.
func assertCatalogHasFocus(t *testing.T, m tea.Model, want bool) {
	t.Helper()
	moved, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, want, moved.View() != m.View(), "catalog focused: %v", want)
}

func TestCtrlCQuitsFromEveryPane(t *testing.T) {
	m := loaded(t, newCatalog(t))
	for i := 0; i < 3; i++ {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
}

func TestQQuitsOutsideTheEditor(t *testing.T) {
	m := loaded(t, newCatalog(t))
	_, cmd := m.Update(keyRune('q'))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestQIsTextInsideTheEditor(t *testing.T) {
	m := loaded(t, newCatalog(t))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyF2})

	_, cmd := m.Update(keyRune('q'))
	assert.Nil(t, cmd, "q belongs to the editor buffer, not the quit binding")
}

func TestHelpOverlayOpensAndCloses(t *testing.T) {
	m := loaded(t, newCatalog(t))

	m, _ = press(t, m, keyRune('?'))
	assert.NotContains(t, m.View(), catalogTitle, "the overlay replaces the layout")

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	assert.Contains(t, m.View(), catalogTitle)
}

func TestF1OpensHelpFromTheEditor(t *testing.T) {
	m := loaded(t, newCatalog(t))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyF2})

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyF1})
	assert.NotContains(t, m.View(), catalogTitle)
}

func TestDisabledBindingsDoNothing(t *testing.T) {
	m := loaded(t, newCatalog(t))

	for _, key := range []tea.KeyMsg{{Type: tea.KeyF5}, {Type: tea.KeyF8}} {
		_, cmd := m.Update(key)
		assert.Nil(t, cmd, "%s is bound but disabled until its iteration lands", key.String())
	}
}

func TestViewNeverOutgrowsTheTerminal(t *testing.T) {
	sizes := []struct {
		width  int
		height int
	}{
		{testWidth, testHeight},
		{120, 40},
		{200, 60},
		{40, 12},
	}
	for _, size := range sizes {
		m := loaded(t, newCatalog(t))
		m, _ = m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		view := m.View()

		assert.LessOrEqual(t, lipgloss.Width(view), size.width, "width at %dx%d", size.width, size.height)
		assert.LessOrEqual(t, lipgloss.Height(view), size.height, "height at %dx%d", size.width, size.height)
	}
}

func TestViewFillsTheMinimumTerminal(t *testing.T) {
	view := loaded(t, newCatalog(t)).View()

	assert.Equal(t, testWidth, lipgloss.Width(view))
	assert.Equal(t, testHeight, lipgloss.Height(view))
	for i, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), testWidth, "line %d wraps", i)
	}
}

func TestViewIsEmptyBeforeTheFirstResize(t *testing.T) {
	m := tui.New(tui.Options{Catalog: newCatalog(t)})

	assert.Empty(t, m.View())
}
