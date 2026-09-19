package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The pane titles as they appear inside the top border.
const (
	catalogTitle = " Catalog "
	editorTitle  = " Editor "
	resultsTitle = " Results "
)

func TestViewShowsEveryPaneAndTheStatusBar(t *testing.T) {
	view := newLoadedModel(t, newConnection(t)).View()

	for _, want := range []string{catalogTitle, editorTitle, resultsTitle} {
		assert.Contains(t, view, want)
	}
	assert.Contains(t, view, mock.Name, "status bar should name the profile")
	assert.Contains(t, view, "no scope", "status bar should say when no container is selected")
	assert.Contains(t, view, "? help")
}

// Focus is only visible in a pane's border color, so these tests use the
// dedicated e binding as the reference for "the editor has focus" and
// compare renders against it.
func TestTabCyclesCatalogThenEditorThenResults(t *testing.T) {
	catalog := newLoadedModel(t, newConnection(t))
	editor := pressAll(t, catalog, keyRune('e'))

	oneTab := pressAll(t, catalog, keyMsg(tea.KeyTab))
	assert.Equal(t, editor.View(), oneTab.View(), "one tab should land on the editor")

	twoTabs := pressAll(t, oneTab, keyMsg(tea.KeyTab))
	assert.NotEqual(t, editor.View(), twoTabs.View())
	assert.NotEqual(t, catalog.View(), twoTabs.View(), "two tabs should land on the results pane")

	threeTabs := pressAll(t, twoTabs, keyMsg(tea.KeyTab))
	assert.Equal(t, catalog.View(), threeTabs.View(), "three tabs should return to the catalog")
}

func TestShiftTabCyclesBackwards(t *testing.T) {
	catalog := newLoadedModel(t, newConnection(t))
	results := pressAll(t, catalog, keyMsg(tea.KeyTab), keyMsg(tea.KeyTab))
	editor := pressAll(t, catalog, keyRune('e'))

	assert.Equal(t, results.View(), pressAll(t, catalog, keyMsg(tea.KeyShiftTab)).View(),
		"one shift+tab should land on the results pane")
	assert.Equal(t, editor.View(), pressAll(t, catalog, keyMsg(tea.KeyShiftTab), keyMsg(tea.KeyShiftTab)).View(),
		"two shift+tabs should land on the editor")
}

func TestOnlyTheFocusedPaneTakesItsKeys(t *testing.T) {
	catalog := newLoadedModel(t, newConnection(t))
	require.NotEqual(t, catalog.View(), pressAll(t, catalog, keyMsg(tea.KeyDown)).View(),
		"the focused catalog moves its cursor")

	editor := pressAll(t, catalog, keyRune('e'))
	assert.Equal(t, editor.View(), pressAll(t, editor, keyMsg(tea.KeyDown)).View(),
		"a blurred catalog ignores the same key")
}

func TestCtrlCQuitsFromEveryPane(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))
	for _, pane := range []string{"catalog", "editor", "results"} {
		_, cmd := m.Update(keyMsg(tea.KeyCtrlC))
		require.NotNil(t, cmd, "in the %s pane", pane)
		assert.IsType(t, tea.QuitMsg{}, cmd(), "in the %s pane", pane)
		m = pressAll(t, m, keyMsg(tea.KeyTab))
	}
}

func TestQQuitsOutsideTheEditor(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	_, cmd := m.Update(keyRune('q'))

	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestTheEditorKeepsPlainKeysAsText(t *testing.T) {
	editor := pressAll(t, newLoadedModel(t, newConnection(t)), keyRune('e'))

	for _, k := range []tea.KeyMsg{keyRune('q'), keyRune('r'), keyRune('?'), keyRune('e'), keySpace()} {
		typed, cmd := editor.Update(k)
		assert.Nil(t, cmd, "%q belongs to the buffer, not to a global shortcut", k.String())
		assert.Contains(t, typed.View(), catalogTitle, "%q must not quit or open an overlay", k.String())
		editor = typed
	}

	assert.Contains(t, editor.View(), "qr?e ", "and every one of them was typed")
}

func TestEscapeReturnsTheEditorToThePreviousPane(t *testing.T) {
	results := pressAll(t, newLoadedModel(t, newConnection(t)), keyMsg(tea.KeyTab), keyMsg(tea.KeyTab))

	returned := pressAll(t, results, keyRune('e'), keyMsg(tea.KeyEscape))

	assert.Equal(t, results.View(), returned.View(), "esc should hand focus back to the results pane")
}

func TestHelpOverlayOpensAndCloses(t *testing.T) {
	m := newLoadedModel(t, newConnection(t))

	m = pressAll(t, m, keyRune('?'))
	assert.NotContains(t, m.View(), catalogTitle, "the overlay replaces the layout")

	m = pressAll(t, m, keyMsg(tea.KeyEscape))
	assert.Contains(t, m.View(), catalogTitle)
}

// ? is text while editing, so help is one esc away from the editor.
func TestHelpOpensFromTheEditorAfterEscape(t *testing.T) {
	editor := pressAll(t, newLoadedModel(t, newConnection(t)), keyRune('e'))

	assert.NotContains(t, pressAll(t, editor, keyMsg(tea.KeyEscape), keyRune('?')).View(), catalogTitle)
}

func TestViewNeverOutgrowsTheTerminal(t *testing.T) {
	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{name: "minimum supported", width: testWidth, height: testHeight},
		{name: "wide", width: 120, height: 40},
		{name: "very wide", width: 200, height: 60},
		{name: "narrower than the catalog wants", width: 40, height: 12},
		{name: "barely a terminal", width: 10, height: 4},
	}
	for _, tt := range sizes {
		t.Run(tt.name, func(t *testing.T) {
			m := newLoadedModel(t, newConnection(t))
			m, _ = m.Update(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})

			view := m.View()
			assert.LessOrEqual(t, lipgloss.Width(view), tt.width)
			assert.LessOrEqual(t, lipgloss.Height(view), tt.height)
		})
	}
}

func TestViewFillsTheMinimumTerminal(t *testing.T) {
	view := newLoadedModel(t, newConnection(t)).View()

	assert.Equal(t, testWidth, lipgloss.Width(view))
	assert.Equal(t, testHeight, lipgloss.Height(view))
	for i, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), testWidth, "line %d wraps", i)
	}
}

func TestViewIsEmptyBeforeTheFirstResize(t *testing.T) {
	m := tui.New(tui.Options{Icons: theme.Icons(), Connection: newConnection(t)})

	assert.Empty(t, m.View())
}
