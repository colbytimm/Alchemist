package panes

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	// maxSuggestions is the most rows the list takes from the buffer.
	maxSuggestions = 6
	// minBufferRows is what the buffer keeps once the list has docked; a
	// pane that cannot spare a list row above that shows the one-line form.
	minBufferRows = 2
	// maxSuggestionWidth keeps one long name from pushing every detail off
	// the pane.
	maxSuggestionWidth = 32
	suggestionGap      = "  "
	selectedMarker     = "▸ "
)

// suggestionHints are the keys the list answers to besides the accept
// binding it is built with; the root model routes them.
var suggestionHints = []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "dismiss")),
}

// Suggestions is the completion list docked under the buffer: rows, a
// selection, and a hint line that says how many there are, or what is still
// being fetched for them.
type Suggestions struct {
	items  []complete.Suggestion
	cursor int
	note   string
	keys   []key.Binding
	hints  help.Model
}

func newSuggestions(accept key.Binding) Suggestions {
	hints := help.New()
	hints.Styles = helpStyles()
	return Suggestions{keys: append([]key.Binding{accept}, suggestionHints...), hints: hints}
}

// Open is true with rows to show, or a note about rows still to come.
func (s Suggestions) Open() bool {
	return len(s.items) > 0 || s.note != ""
}

// Set replaces the rows and the note. The selection stays on the row it was
// on when that row is still listed, since a refresh the user did not ask for
// must not move it; otherwise it returns to the first row.
func (s Suggestions) Set(items []complete.Suggestion, note string) Suggestions {
	selected, _ := s.Selected()
	s.items, s.note = items, note
	s.cursor = max(slices.IndexFunc(items, func(item complete.Suggestion) bool { return item.Text == selected.Text }), 0)
	return s
}

func (s Suggestions) Clear() Suggestions {
	return s.Set(nil, "")
}

func (s Suggestions) CursorUp() Suggestions {
	s.cursor = max(s.cursor-1, 0)
	return s
}

func (s Suggestions) CursorDown() Suggestions {
	s.cursor = min(s.cursor+1, max(len(s.items)-1, 0))
	return s
}

func (s Suggestions) Selected() (complete.Suggestion, bool) {
	if len(s.items) == 0 {
		return complete.Suggestion{}, false
	}
	return s.items[s.cursor], true
}

// rows is how many of the inner rows the list takes: as many of its rows as
// the buffer can spare and the hint line, or one line when it can spare
// none.
func (s Suggestions) rows(inner int) int {
	if !s.Open() {
		return 0
	}
	spare := inner - hintLines - minBufferRows
	if spare < 1 {
		return 1
	}
	return min(len(s.items), maxSuggestions, spare) + hintLines
}

func (s Suggestions) lines(width, height int) []string {
	if height <= 1 {
		return []string{clipLine(s.oneLine(), width)}
	}
	lines := s.rowLines(width, height-hintLines)
	return append(lines, s.hintLine(width))
}

// oneLine is the selected suggestion and the count on one row.
func (s Suggestions) oneLine() string {
	selected, ok := s.Selected()
	if !ok {
		return theme.HintStyle().Render(s.note)
	}
	line := theme.SelectedStyle().Render(selectedMarker+selected.Text) + suggestionGap + theme.HintStyle().Render(selected.Detail)
	return line + suggestionGap + theme.HintStyle().Render(s.count())
}

func (s Suggestions) rowLines(width, rows int) []string {
	start, end := windowBounds(len(s.items), s.cursor, rows)
	nameWidth := 0
	for _, item := range s.items[start:end] {
		nameWidth = max(nameWidth, lipgloss.Width(item.Text))
	}
	nameWidth = min(nameWidth, maxSuggestionWidth)
	lines := make([]string, 0, rows)
	for i, item := range s.items[start:end] {
		lines = append(lines, clipLine(s.rowLine(item, nameWidth, start+i == s.cursor), width))
	}
	return lines
}

func (s Suggestions) rowLine(item complete.Suggestion, nameWidth int, selected bool) string {
	marker := strings.Repeat(" ", lipgloss.Width(selectedMarker))
	name := theme.TextStyle().Render(fit(item.Text, nameWidth))
	if selected {
		marker = selectedMarker
		name = theme.SelectedStyle().Render(fit(item.Text, nameWidth))
	}
	return marker + name + suggestionGap + theme.HintStyle().Render(item.Detail)
}

// hintLine shows the keys, or the note while a fetch is out, with the
// selection's place at the right edge. The count survives a narrow pane;
// the keys are cut to make room for it.
func (s Suggestions) hintLine(width int) string {
	left := s.hints.ShortHelpView(s.keys)
	if s.note != "" {
		left = theme.HintStyle().Render(s.note)
	}
	right := theme.HintStyle().Render(s.count())
	left = ansi.Truncate(left, max(width-lipgloss.Width(right)-len(suggestionGap), 0), "…")
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	return left + strings.Repeat(" ", max(gap, 0)) + right
}

func (s Suggestions) count() string {
	if len(s.items) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d", s.cursor+1, len(s.items))
}

func clipLine(line string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
