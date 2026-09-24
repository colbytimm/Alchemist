package panes_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// editorWidth is wide enough for the placeholder to render in full.
const editorWidth = 44

func typed(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

func TestEditorHintsTheScopeSyntaxWhileEmpty(t *testing.T) {
	editor := panes.NewEditor(accept).SetSize(editorWidth, paneHeight)

	assert.Contains(t, plain(editor.View()), "SELECT * FROM db.container AS c")
	assert.Empty(t, editor.Value())
}

func TestEditorTakesTypedText(t *testing.T) {
	editor, _ := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).Focus().Update(typed("SELECT 1"))

	assert.Equal(t, "SELECT 1", editor.Value())
	assert.Contains(t, plain(editor.View()), "SELECT 1")
}

func TestABlurredEditorIgnoresTyping(t *testing.T) {
	editor, _ := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).Blur().Update(typed("SELECT 1"))

	assert.Empty(t, editor.Value(), "only the focused pane takes keys")
}

// A query long enough to wrap at editorWidth, with a string that spans the
// line break typed into it.
const highlightedQuery = "SELECT TOP 5 * FROM c WHERE c.name = 'two\nlines' AND c.total > 1.5"

// tallQuery has more lines than the pane has rows.
var tallQuery = tallQueryText()

func tallQueryText() string {
	lines := []string{"SELECT * FROM c WHERE c.n = 0"}
	for n := 1; n < 30; n++ {
		lines = append(lines, fmt.Sprintf("  OR c.n = %d", n))
	}
	return strings.Join(lines, "\n")
}

// scrolledToEnd holds tallQuery with its last line on screen. The textarea
// scrolls to its cursor only on a key that arrives after a frame has been
// drawn, which is the order a running program delivers them in.
func scrolledToEnd(t *testing.T) panes.Editor {
	t.Helper()
	editor := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).Focus().SetValue(tallQuery)
	editor.View()
	editor, _ = editor.Update(typed(" "))
	require.NotContains(t, plain(editor.View()), "SELECT", "the first line should have scrolled away")
	return editor
}

func colored(color lipgloss.AdaptiveColor, text string) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

func TestABlurredEditorColorsKeywordsStringsAndNumbers(t *testing.T) {
	view := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).SetValue(highlightedQuery).Blur().View()

	assert.Contains(t, view, colored(theme.Amethyst(), "SELECT"))
	assert.Contains(t, view, colored(theme.Verdigris(), "'two"), "a string is colored on every line it covers")
	assert.Contains(t, view, colored(theme.Verdigris(), "lines'"))
	assert.Contains(t, view, colored(theme.Copper(), "1.5"))
}

func TestABlurredEditorDimsCommentsAndTheQuotesInsideThem(t *testing.T) {
	view := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).
		SetValue("SELECT * -- the customer's\nFROM c").Blur().View()

	assert.Contains(t, view, colored(theme.Ash(), "-- the customer's"))
	assert.Contains(t, view, colored(theme.Amethyst(), "FROM"), "the apostrophe opened no string")
}

func TestAFocusedEditorStaysPlain(t *testing.T) {
	view := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).SetValue(highlightedQuery).Focus().View()

	assert.NotContains(t, view, colored(theme.Amethyst(), "SELECT"))
}

func TestHighlightingMovesNoText(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "a wrapped query with a string across lines", query: highlightedQuery},
		{name: "hyphens, where word wrappers disagree", query: "SELECT * FROM c WHERE c.id = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'"},
		{name: "a line as wide as the pane", query: "SELECT * FROM c WHERE c.name = 'abcdefgh'"},
		{name: "indented continuation lines", query: "SELECT *\n  FROM c\n    WHERE c.n > 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).Focus().SetValue(tt.query)

			assert.Equal(t, plain(editor.View()), plain(editor.Blur().View()))
		})
	}
}

func TestHighlightingKeepsTheScrollPosition(t *testing.T) {
	editor := scrolledToEnd(t)

	assert.Equal(t, plain(editor.View()), plain(editor.Blur().View()))
}

func TestAScrolledEditorStillColorsWhatItShows(t *testing.T) {
	view := scrolledToEnd(t).Blur().View()

	assert.Contains(t, view, colored(theme.Copper(), "29"))
	assert.Contains(t, view, colored(theme.Amethyst(), "OR"))
}

func TestASpaceTheTextareaRedrawsStillColors(t *testing.T) {
	view := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).SetValue("SELECT * FROM c").Blur().View()

	assert.Contains(t, view, colored(theme.Amethyst(), "SELECT"))
}
