package panes

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const filterPrompt = "/ "

// filterList is the list an overlay narrows by typing: the items, the filter
// line over them, and a cursor among the ones that match. Like the input it
// wraps, its value receiver hides shared pointers, so a caller must keep every
// filterList it is handed.
type filterList[T any] struct {
	filter  textinput.Model
	items   []T
	cursor  int
	matches func(item T, needle string) bool
}

// newFilterList builds a list whose filter line shows hint while empty, and
// which keeps an item when matches reports it contains the lower-cased needle.
func newFilterList[T any](hint string, matches func(item T, needle string) bool) filterList[T] {
	filter := newInput(hint, "")
	filter.Prompt = filterPrompt
	return filterList[T]{filter: filter, matches: matches}
}

func (l filterList[T]) setWidth(width int) filterList[T] {
	l.filter.Width = max(width-lipgloss.Width(filterPrompt), 1)
	return l
}

func (l filterList[T]) setItems(items []T) filterList[T] {
	l.items = items
	return l
}

func (l filterList[T]) startFilter() filterList[T] {
	l.filter.Focus()
	return l
}

func (l filterList[T]) filtering() bool {
	return l.filter.Focused()
}

func (l filterList[T]) clearFilter() filterList[T] {
	l.filter.Reset()
	l.filter.Blur()
	l.cursor = 0
	return l
}

// update types into the filter line. The cursor returns to the first match
// when the text changes, since the row it was on may no longer be listed.
func (l filterList[T]) update(msg tea.KeyMsg) (filterList[T], tea.Cmd) {
	before := l.filter.Value()
	var cmd tea.Cmd
	l.filter, cmd = l.filter.Update(msg)
	if l.filter.Value() != before {
		l.cursor = 0
	}
	return l, cmd
}

func (l filterList[T]) moveCursor(delta int) filterList[T] {
	count := len(l.matching())
	if count == 0 {
		return l
	}
	l.cursor = min(max(l.cursor+delta, 0), count-1)
	return l
}

// placeCursor puts the cursor on the first match found reports true for, and
// leaves it where it is when there is none.
func (l filterList[T]) placeCursor(found func(T) bool) filterList[T] {
	for i, item := range l.matching() {
		if found(item) {
			l.cursor = i
			return l
		}
	}
	return l
}

func (l filterList[T]) selected() (T, bool) {
	matching := l.matching()
	if l.cursor >= len(matching) {
		var none T
		return none, false
	}
	return matching[l.cursor], true
}

func (l filterList[T]) matching() []T {
	needle := strings.ToLower(strings.TrimSpace(l.filter.Value()))
	if needle == "" {
		return l.items
	}
	var matching []T
	for _, item := range l.items {
		if l.matches(item, needle) {
			matching = append(matching, item)
		}
	}
	return matching
}

func (l filterList[T]) filterLine() string {
	return promptedInputView(l.filter)
}

// padBody makes lines exactly height long, so the hint line under them stays
// at the bottom of the frame.
func padBody(lines []string, height int) []string {
	body := make([]string, height)
	copy(body, lines)
	return body
}
