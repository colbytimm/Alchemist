package panes_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// listWidth fits the list's whole hint line.
const listWidth = 60

func focusedEditor(value string) panes.Editor {
	return panes.NewEditor(accept).SetSize(listWidth, paneHeight).Focus().SetValue(value)
}

func fields(names ...string) []complete.Suggestion {
	suggestions := make([]complete.Suggestion, 0, len(names))
	for _, name := range names {
		suggestions = append(suggestions, complete.Suggestion{Text: name, Insert: name, Kind: complete.KindField, Detail: "string"})
	}
	return suggestions
}

func TestCursorReportsAByteOffset(t *testing.T) {
	tests := []struct {
		name  string
		value string
		keys  []tea.KeyMsg
		want  int
	}{
		{name: "at the end", value: "SELECT 1", want: 8},
		{name: "on a second line", value: "SELECT *\nFROM c", want: 15},
		{name: "moved left over a wide rune", value: "SELECT 'Zoë'", keys: []tea.KeyMsg{{Type: tea.KeyLeft}, {Type: tea.KeyLeft}}, want: len("SELECT 'Zo")},
		{name: "moved up a line", value: "SELECT *\nFROM c", keys: []tea.KeyMsg{{Type: tea.KeyUp}}, want: 6},
		{name: "empty", value: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := focusedEditor(tt.value)
			for _, k := range tt.keys {
				editor, _ = editor.Update(k)
			}

			text, offset := editor.Cursor()

			assert.Equal(t, tt.value, text)
			assert.Equal(t, tt.want, offset)
		})
	}
}

func TestReplaceRewritesTheRangeAndLeavesTheCursorAfterIt(t *testing.T) {
	tests := []struct {
		name  string
		value string
		start int
		end   int
		with  string
		want  string
	}{
		{name: "one line", value: "SELECT c.cu FROM c", start: 9, end: 11, with: "customerId", want: "SELECT c.customerId| FROM c"},
		{name: "a later line", value: "SELECT *\nFROM c\nWHERE c.cu", start: 24, end: 26, with: "currency", want: "SELECT *\nFROM c\nWHERE c.currency|"},
		{name: "an earlier line", value: "SEL\nFROM c", start: 0, end: 3, with: "SELECT", want: "SELECT|\nFROM c"},
		{name: "after wide runes", value: "SELECT 'Zoë', c.cu", start: 17, end: 19, with: "customerId", want: "SELECT 'Zoë', c.customerId|"},
		{name: "an empty range inserts", value: "SELECT c.", start: 9, end: 9, with: "id", want: "SELECT c.id|"},
		{name: "a bracket form takes the dot", value: "SELECT c.ord", start: 8, end: 12, with: `["order-id"]`, want: `SELECT c["order-id"]|`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := focusedEditor(tt.value).Replace(tt.start, tt.end, tt.with)

			text, offset := editor.Cursor()
			assert.Equal(t, tt.want, text[:offset]+"|"+text[offset:])

			typed, _ := editor.Update(typed("X"))
			assert.Equal(t, strings.Replace(tt.want, "|", "X", 1), typed.Value(), "typing continues at the cursor")
		})
	}
}

func TestReplaceClampsARangeOutsideTheBuffer(t *testing.T) {
	editor := focusedEditor("SELECT").Replace(-3, 99, "x")

	assert.Equal(t, "x", editor.Value())
}

func TestSuggestionsDockUnderTheBufferWithAHintLine(t *testing.T) {
	editor := focusedEditor("SELECT c.cu").SetSuggestions(fields("customerId", "currency", "customer"), "")

	view := plain(editor.View())
	require.True(t, editor.Suggesting())
	assert.Contains(t, view, "SELECT c.cu", "the buffer stays")
	assert.Contains(t, view, "▸ customerId")
	assert.Contains(t, view, "  currency")
	assert.Contains(t, view, "tab accept")
	assert.Contains(t, view, "↑/↓ choose")
	assert.Contains(t, view, "esc dismiss")
	assert.Contains(t, view, "1 of 3")

	selected, ok := editor.NextSuggestion().Selected()
	require.True(t, ok)
	assert.Equal(t, "currency", selected.Text)
	assert.Contains(t, plain(editor.NextSuggestion().View()), "2 of 3")
	assert.Contains(t, plain(editor.NextSuggestion().PrevSuggestion().View()), "1 of 3")
}

func TestSuggestionsNeverPushTheFrameTaller(t *testing.T) {
	editor := focusedEditor("SELECT c.cu")
	closed := editor.View()

	open := editor.SetSuggestions(fields("a", "b", "c", "d", "e", "f", "g", "h"), "").View()

	assert.Equal(t, strings.Count(closed, "\n"), strings.Count(open, "\n"))
	assert.Contains(t, plain(open), "▸ a")
	assert.NotContains(t, plain(open), "  g", "at most six rows")
	assert.Contains(t, plain(editor.SetSuggestions(fields("a", "b", "c", "d", "e", "f", "g", "h"), "").
		NextSuggestion().NextSuggestion().NextSuggestion().NextSuggestion().NextSuggestion().NextSuggestion().View()), "▸ g",
		"the window follows the selection")
}

func TestANoteReplacesTheKeyHints(t *testing.T) {
	editor := focusedEditor("SELECT c.cu").SetSuggestions(fields("customerId"), "sampling orders…")

	view := plain(editor.View())
	assert.Contains(t, view, "sampling orders…")
	assert.NotContains(t, view, "tab accept")

	waiting := focusedEditor("SELECT c.cu").SetSuggestions(nil, "sampling orders…")
	assert.Contains(t, plain(waiting.View()), "sampling orders…", "a note shows with nothing to list yet")
	assert.False(t, waiting.Suggesting(), "but there is nothing to accept")
}

func TestAShortPaneShowsTheOneLineForm(t *testing.T) {
	editor := panes.NewEditor(accept).SetSize(listWidth, 5).Focus().SetValue("SELECT c.cu").
		SetSuggestions(fields("customerId", "currency"), "")

	view := plain(editor.View())
	assert.Contains(t, view, "▸ customerId")
	assert.Contains(t, view, "1 of 2")
	assert.NotContains(t, view, "currency")
	assert.Contains(t, view, "SELECT c.cu", "the buffer keeps its rows")
	assert.Equal(t, 5, strings.Count(view, "\n")+1)
}

func TestANarrowPaneKeepsTheCountAndCutsTheKeys(t *testing.T) {
	editor := panes.NewEditor(accept).SetSize(editorWidth, paneHeight).Focus().SetValue("SELECT c.cu").
		SetSuggestions(fields("customerId", "currency"), "")

	view := plain(editor.View())
	assert.Contains(t, view, "1 of 2")
	assert.Contains(t, view, "tab accept")
	assert.NotContains(t, view, "esc dismiss")
}

func TestClearingAndBlurringCloseTheList(t *testing.T) {
	editor := focusedEditor("SELECT c.cu").SetSuggestions(fields("customerId"), "")

	assert.False(t, editor.ClearSuggestions().Suggesting())
	assert.NotContains(t, plain(editor.ClearSuggestions().View()), "customerId")
	assert.False(t, editor.Blur().Suggesting())
}

func TestDockingAndUndockingKeepTheCursorLineOnScreen(t *testing.T) {
	editor := panes.NewEditor(accept).SetSize(listWidth, 7).Focus().SetValue("SELECT *\nFROM c\nWHERE c.a = 1\nAND c.b")

	docked := editor.SetSuggestions(fields("customerId", "currency"), "")
	assert.Contains(t, plain(docked.View()), "AND c.b", "the buffer scrolls to keep the cursor line")

	undocked := docked.ClearSuggestions()
	view := plain(undocked.View())
	assert.Contains(t, view, "SELECT *", "the buffer scrolls back up once the rows return")
	assert.Contains(t, view, "AND c.b")
}

func TestARefreshKeepsTheSelectedRow(t *testing.T) {
	editor := focusedEditor("SELECT c.").SetSuggestions(fields("customerId", "currency", "customer"), "").NextSuggestion()

	refreshed := editor.SetSuggestions(fields("customerId", "currency", "customer", "created"), "sampling orders…")
	selected, ok := refreshed.Selected()
	require.True(t, ok)
	assert.Equal(t, "currency", selected.Text)

	gone := refreshed.SetSuggestions(fields("customerId", "created"), "")
	selected, ok = gone.Selected()
	require.True(t, ok)
	assert.Equal(t, "customerId", selected.Text, "a row no longer listed hands the selection to the first")
}
