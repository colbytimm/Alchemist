package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/saved"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	savedTitle       = "Saved queries"
	savedFilterHint  = "filter by name, query or scope"
	nothingSavedHint = "nothing saved for %s yet: ctrl+s saves the query in the editor"
	keepHint         = "any other key keeps it"
	// maxNameWidth stops one long name from crowding out the query.
	maxNameWidth = 32
	// maxPreviewLines is as much of the selected query as the preview shows.
	maxPreviewLines = 8
	// minListRows is the least the list keeps before the preview gives way.
	minListRows = 3
	// savedRowGaps is the leading space and the three between columns.
	savedRowGaps = 4
	previewRule  = "─"
)

// Saved is the overlay of one account's saved queries. Like the filter input
// it wraps, its value receiver hides shared pointers, so a caller must keep
// every Saved it is handed.
type Saved struct {
	frame    frame
	icons    theme.IconSet
	hints    help.Model
	keys     []key.Binding
	confirm  key.Binding
	list     filterList[saved.Query]
	account  string
	skipped  int
	now      time.Time
	failure  string
	deleting bool
}

// NewSaved builds the overlay; keys are the bindings its hint line shows, and
// confirm the one that answers its question before a delete.
func NewSaved(icons theme.IconSet, keys []key.Binding, confirm key.Binding) Saved {
	hints := help.New()
	return Saved{
		frame:   frame{title: savedTitle, focused: true},
		icons:   icons,
		hints:   hints,
		keys:    keys,
		confirm: confirm,
		list:    newFilterList(savedFilterHint, matchesSaved),
	}
}

func (s Saved) SetSize(width, height int) Saved {
	s.frame = s.frame.size(width, height)
	width, _ = s.frame.inner()
	s.list = s.list.setWidth(width)
	s.hints.Width = width
	return s
}

// SetAccount titles the overlay for account and empties it until its listing
// arrives. An empty account is none.
func (s Saved) SetAccount(account string) Saved {
	s.frame.title = accountTitle(savedTitle, account)
	s.account = account
	s.list = s.list.setItems(nil).clearFilter()
	s.skipped = 0
	s.failure = ""
	s.deleting = false
	return s
}

// SetListing shows what the directory of account held, aged against now. The
// filter stays, and the cursor stays on the query of the same name while it
// is still listed; otherwise on its neighbor.
func (s Saved) SetListing(account string, listing saved.Listing, now time.Time) Saved {
	selected, hadSelection := s.list.selected()
	s.frame.title = accountTitle(savedTitle, account)
	s.account = account
	s.list = s.list.setItems(listing.Queries)
	s.skipped = len(listing.Skipped)
	s.now = now
	s.failure = ""
	s.deleting = false
	if hadSelection {
		s = s.Select(selected.Name)
	}
	s.list = s.list.moveCursor(0)
	return s
}

// Select puts the cursor on the query called name, when it is listed.
func (s Saved) Select(name string) Saved {
	s.list = s.list.placeCursor(func(q saved.Query) bool { return strings.EqualFold(q.Name, name) })
	return s
}

// Fail shows why the queries could not be listed, in place of them.
func (s Saved) Fail(err error) Saved {
	s.list = s.list.setItems(nil)
	s.skipped = 0
	s.failure = err.Error()
	s.deleting = false
	return s
}

func (s Saved) StartFilter() Saved {
	s.list = s.list.startFilter()
	return s
}

// Filtering reports whether typed characters go to the filter line.
func (s Saved) Filtering() bool {
	return s.list.filtering()
}

func (s Saved) ClearFilter() Saved {
	s.list = s.list.clearFilter()
	return s
}

func (s Saved) Update(msg tea.KeyMsg) (Saved, tea.Cmd) {
	var cmd tea.Cmd
	s.list, cmd = s.list.update(msg)
	return s, cmd
}

func (s Saved) CursorUp() Saved {
	s.list = s.list.moveCursor(-1)
	return s
}

func (s Saved) CursorDown() Saved {
	s.list = s.list.moveCursor(1)
	return s
}

func (s Saved) Selected() (saved.Query, bool) {
	return s.list.selected()
}

// AskDelete asks whether to delete the selected query.
func (s Saved) AskDelete() Saved {
	_, ok := s.list.selected()
	s.deleting = ok
	return s
}

// Deleting reports whether the overlay is asking about a delete.
func (s Saved) Deleting() bool {
	return s.deleting
}

func (s Saved) CancelDelete() Saved {
	s.deleting = false
	return s
}

func (s Saved) View() string {
	width, height := s.frame.inner()
	footer := s.footer(width)
	preview := s.preview(width, height-chromeLines-len(footer))
	listHeight := max(height-chromeLines-len(footer)-len(preview), 0)
	lines := []string{s.list.filterLine()}
	lines = append(lines, padBody(s.rows(width, listHeight), listHeight)...)
	lines = append(lines, preview...)
	lines = append(lines, footer...)
	lines = append(lines, s.hintLine())
	return s.frame.render(strings.Join(lines, "\n"))
}

func matchesSaved(q saved.Query, needle string) bool {
	return strings.Contains(strings.ToLower(q.Name), needle) ||
		strings.Contains(strings.ToLower(q.Text), needle) ||
		strings.Contains(strings.ToLower(scopeText(q.Scope)), needle)
}

// rows renders only the rows that fit, since View runs on every frame.
func (s Saved) rows(width, height int) []string {
	switch {
	case s.failure != "":
		return []string{theme.ErrorStyle().Render(s.icons.Failure + " " + s.failure)}
	case len(s.list.items) == 0:
		return []string{theme.HintStyle().Render(fmt.Sprintf(nothingSavedHint, s.account))}
	}
	matches := s.list.matching()
	if len(matches) == 0 {
		return []string{theme.HintStyle().Render(noMatchHint)}
	}
	columns := measureSavedColumns(matches)
	start, end := windowBounds(len(matches), s.list.cursor, height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, s.row(matches[i], columns, width, i == s.list.cursor))
	}
	return lines
}

// savedColumns are the widths of the name and scope columns.
type savedColumns struct {
	name, scope int
}

func measureSavedColumns(queries []saved.Query) savedColumns {
	var columns savedColumns
	for _, q := range queries {
		columns.name = max(columns.name, lipgloss.Width(q.Name))
		columns.scope = max(columns.scope, lipgloss.Width(scopeText(q.Scope)))
	}
	return savedColumns{name: min(columns.name, maxNameWidth), scope: min(columns.scope, maxScopeWidth)}
}

func (s Saved) row(q saved.Query, columns savedColumns, width int, selected bool) string {
	style := theme.TextStyle()
	if selected {
		style = theme.SelectedStyle()
	}
	age := fmt.Sprintf("%*s", timeWidth, relativeTime(q.Saved, s.now))
	query := fit(firstLine(q.Text), max(width-columns.name-columns.scope-timeWidth-savedRowGaps, 1))
	return style.Render(" " + fit(q.Name, columns.name) + " " + fit(scopeText(q.Scope), columns.scope) + " " + query + " " + age)
}

// preview is the whole text of the selected query under a rule, and nothing
// when showing it would leave the list fewer than minListRows rows.
func (s Saved) preview(width, room int) []string {
	q, ok := s.list.selected()
	if !ok || s.failure != "" {
		return nil
	}
	text := strings.Split(q.Text, "\n")
	text = text[:min(len(text), maxPreviewLines)]
	if room-len(text)-1 < minListRows {
		return nil
	}
	lines := []string{theme.HintStyle().Render(strings.Repeat(previewRule, width))}
	for _, line := range text {
		lines = append(lines, theme.TextStyle().Render(fit(line, width)))
	}
	return lines
}

// footer says how many files the listing had to skip, when any.
func (s Saved) footer(width int) []string {
	if s.skipped == 0 {
		return nil
	}
	files := "files"
	if s.skipped == 1 {
		files = "file"
	}
	return []string{theme.HintStyle().Render(fit(fmt.Sprintf("%d %s skipped, see the log", s.skipped, files), width))}
}

// hintLine becomes the question while a delete waits for its answer.
func (s Saved) hintLine() string {
	q, ok := s.list.selected()
	if !s.deleting || !ok {
		return themedHelp(s.hints).ShortHelpView(s.keys)
	}
	question := theme.TextStyle().Render(fmt.Sprintf("delete %q?  ", q.Name))
	return question + themedHelp(s.hints).ShortHelpView([]key.Binding{s.confirm}) + theme.HintStyle().Render("   "+keepHint)
}
