package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	historyTitle  = "History"
	noHistoryHint = "no queries recorded yet"
	noMatchHint   = "nothing matches"
	filterPrompt  = "/ "
	filterHint    = "filter by query or scope"
	// timeWidth fits the widest age label, which is a date.
	timeWidth   = 10
	chargeWidth = 10
	// maxScopeWidth stops a long db.container from crowding out the query.
	maxScopeWidth = 24
	// rowGaps is what a row spends beside its columns: the outcome glyph and
	// the four spaces between columns.
	rowGaps = 5
	// chromeLines is the filter line above the entries and the hint below.
	chromeLines = 2
)

const (
	day       = 24 * time.Hour
	dateAfter = 30 * day
)

// History is the overlay of past runs. Like the filter input it wraps, its
// value receiver hides shared pointers, so a caller must keep every History
// it is handed.
type History struct {
	frame   frame
	icons   theme.IconSet
	hints   help.Model
	keys    []key.Binding
	filter  textinput.Model
	entries []history.Entry
	now     time.Time
	cursor  int
	failure string
}

// NewHistory builds the overlay; keys are the bindings its hint line shows.
func NewHistory(icons theme.IconSet, keys []key.Binding) History {
	hints := help.New()
	hints.Styles = helpStyles()
	filter := newInput(filterHint, "")
	filter.Prompt = filterPrompt
	filter.PromptStyle = theme.HintStyle()
	return History{
		frame:  frame{title: historyTitle, focused: true},
		icons:  icons,
		hints:  hints,
		keys:   keys,
		filter: filter,
	}
}

func (h History) SetSize(width, height int) History {
	h.frame = h.frame.size(width, height)
	width, _ = h.frame.inner()
	h.filter.Width = max(width-lipgloss.Width(filterPrompt), 1)
	h.hints.Width = width
	return h
}

// SetEntries shows entries in the order given, aged against now. The filter
// and cursor start over: the overlay opens on the whole log each time.
func (h History) SetEntries(entries []history.Entry, now time.Time) History {
	h.entries = entries
	h.now = now
	h.failure = ""
	return h.ClearFilter()
}

// Fail shows why the log could not be read, in place of its entries.
func (h History) Fail(err error) History {
	h.entries = nil
	h.failure = err.Error()
	return h.ClearFilter()
}

func (h History) StartFilter() History {
	h.filter.Focus()
	return h
}

// Filtering reports whether typed characters go to the filter line.
func (h History) Filtering() bool {
	return h.filter.Focused()
}

func (h History) ClearFilter() History {
	h.filter.Reset()
	h.filter.Blur()
	h.cursor = 0
	return h
}

// Update types into the filter line. The cursor returns to the first match
// when the text changes, since the row it was on may no longer be listed.
func (h History) Update(msg tea.KeyMsg) (History, tea.Cmd) {
	before := h.filter.Value()
	var cmd tea.Cmd
	h.filter, cmd = h.filter.Update(msg)
	if h.filter.Value() != before {
		h.cursor = 0
	}
	return h, cmd
}

func (h History) CursorUp() History {
	return h.moveCursor(-1)
}

func (h History) CursorDown() History {
	return h.moveCursor(1)
}

func (h History) Selected() (history.Entry, bool) {
	matches := h.matches()
	if h.cursor >= len(matches) {
		return history.Entry{}, false
	}
	return matches[h.cursor], true
}

func (h History) View() string {
	width, height := h.frame.inner()
	lines := []string{h.filter.View()}
	lines = append(lines, h.body(width, max(height-chromeLines, 0))...)
	lines = append(lines, h.hints.ShortHelpView(h.keys))
	return h.frame.render(strings.Join(lines, "\n"))
}

func (h History) moveCursor(delta int) History {
	count := len(h.matches())
	if count == 0 {
		return h
	}
	h.cursor = min(max(h.cursor+delta, 0), count-1)
	return h
}

func (h History) matches() []history.Entry {
	needle := strings.ToLower(strings.TrimSpace(h.filter.Value()))
	if needle == "" {
		return h.entries
	}
	var matches []history.Entry
	for _, entry := range h.entries {
		if matchesFilter(entry, needle) {
			matches = append(matches, entry)
		}
	}
	return matches
}

func matchesFilter(entry history.Entry, needle string) bool {
	return strings.Contains(strings.ToLower(entry.Query), needle) ||
		strings.Contains(strings.ToLower(scopeText(entry.Scope)), needle)
}

// body is exactly height lines: the rows that keep the cursor on screen,
// padded so the hint line stays at the bottom of the frame.
func (h History) body(width, height int) []string {
	lines := make([]string, height)
	copy(lines, h.rows(width, height))
	return lines
}

// rows renders only the rows that fit, since View runs on every frame.
func (h History) rows(width, height int) []string {
	switch {
	case h.failure != "":
		return []string{theme.ErrorStyle().Render(h.icons.Failure + " " + h.failure)}
	case len(h.entries) == 0:
		return []string{theme.HintStyle().Render(noHistoryHint)}
	}
	matches := h.matches()
	if len(matches) == 0 {
		return []string{theme.HintStyle().Render(noMatchHint)}
	}
	scopeWidth := scopeWidth(matches)
	start, end := windowBounds(len(matches), h.cursor, height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, h.row(matches[i], scopeWidth, width, i == h.cursor))
	}
	return lines
}

func (h History) row(entry history.Entry, scopeWidth, width int, selected bool) string {
	text := theme.TextStyle()
	if selected {
		text = theme.SelectedStyle()
	}
	age := fit(relativeTime(entry.Time, h.now), timeWidth)
	scope := fit(scopeText(entry.Scope), scopeWidth)
	charge := fmt.Sprintf("%*s", chargeWidth, chargeText(entry))
	query := fit(firstLine(entry.Query), max(width-timeWidth-scopeWidth-chargeWidth-rowGaps, 1))
	return text.Render(age+" ") + h.outcome(entry, selected) + text.Render(" "+scope+" "+query+" "+charge)
}

func (h History) outcome(entry history.Entry, selected bool) string {
	style, glyph := theme.SuccessStyle(), h.icons.Success
	if !entry.OK {
		style, glyph = theme.ErrorStyle(), h.icons.Failure
	}
	return style.Bold(selected).Render(glyph)
}

func scopeWidth(entries []history.Entry) int {
	width := 0
	for _, entry := range entries {
		width = max(width, lipgloss.Width(scopeText(entry.Scope)))
	}
	return min(width, maxScopeWidth)
}

func scopeText(scope []string) string {
	return strings.Join(scope, ".")
}

// chargeText reports the request charge of a run that produced a page; a
// failed one has none worth reading.
func chargeText(entry history.Entry) string {
	if !entry.OK {
		return pending
	}
	return fmt.Sprintf("%.2f RU", entry.RequestCharge)
}

// firstLine is the one line of a query a row has room for.
func firstLine(query string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(query), "\n")
	return strings.TrimRight(line, " \t\r")
}

func relativeTime(t, now time.Time) string {
	age := now.Sub(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	case age < day:
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	case age < dateAfter:
		return fmt.Sprintf("%dd ago", int(age/day))
	}
	return t.Local().Format(time.DateOnly)
}
