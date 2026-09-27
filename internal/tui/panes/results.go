package panes

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	resultsTitle = "Results"
	resultsHint  = "no results yet"
	noRowsHint   = "no rows"
	// maxColumnWidth stops one long value from pushing every other column off
	// the pane. Width the columns on screen leave over is handed back to
	// the ones it cut short.
	maxColumnWidth = 24
	columnGap      = "  "
)

// Results renders the loaded result set: a sticky header, a row cursor, and
// as many columns as fit from the horizontal scroll offset. A failure is
// rendered above whatever rows are already on screen.
type Results struct {
	frame   frame
	source  string
	outcome string
	banner  string
	// bannerFailed styles the banner, and failedRow, as a failure; failedRow
	// is -1 when no row failed.
	bannerFailed bool
	failedRow    int
	columns      []string
	// widths are each column's widest value, before maxColumnWidth.
	widths   []int
	rows     [][]string
	raw      []json.RawMessage
	failure  string
	cursor   int
	firstCol int
	loaded   bool
}

func NewResults() Results {
	return Results{frame: frame{title: resultsTitle}, failedRow: -1}
}

func (r Results) SetSize(width, height int) Results {
	r.frame = r.frame.size(width, height)
	return r
}

func (r Results) Focus() Results {
	r.frame = r.frame.focus()
	return r
}

func (r Results) Blur() Results {
	r.frame = r.frame.blur()
	return r
}

// SetSource names the account the rows on screen came from, which a switch
// to another account leaves standing.
func (r Results) SetSource(account string) Results {
	r.source = account
	return r.retitle()
}

// SetOutcome names, beside the account, what became of the statement whose
// report is on screen: "committed".
func (r Results) SetOutcome(outcome string) Results {
	r.outcome = outcome
	return r.retitle()
}

// SetBanner shows text above the rows, as a failure when failed.
func (r Results) SetBanner(text string, failed bool) Results {
	r.banner, r.bannerFailed = text, failed
	return r
}

// MarkFailedRow styles row as the one that failed, and puts the cursor on
// it.
func (r Results) MarkFailedRow(row int) Results {
	r.failedRow = row
	r.cursor = min(max(row, 0), max(len(r.rows)-1, 0))
	return r
}

func (r Results) retitle() Results {
	r.frame.title = accountTitle(accountTitle(resultsTitle, r.source), r.outcome)
	return r
}

// Clear empties the pane for a new run.
func (r Results) Clear() Results {
	return Results{frame: r.frame, source: r.source, failedRow: -1}.retitle()
}

// Load replaces the result set with the first page of a run.
func (r Results) Load(page adapter.Page) Results {
	r = r.Clear()
	r.columns = page.Columns
	r.rows = page.Rows
	r.raw = page.Raw
	r.widths = columnWidths(r.columns, r.rows)
	r.loaded = true
	return r
}

// Append extends the result set with a further page. Adapters lock the column
// order on the first page and only ever add columns at the end, so a longer
// list from a later page extends the header without disturbing it.
func (r Results) Append(page adapter.Page) Results {
	if len(page.Columns) > len(r.columns) {
		r.columns = page.Columns
	}
	r.rows = slices.Concat(r.rows, page.Rows)
	r.raw = slices.Concat(r.raw, page.Raw)
	r.widths = columnWidths(r.columns, r.rows)
	return r
}

// Fail records err for display. Rows already fetched stay on screen: a page
// that failed to arrive says nothing about the ones that did.
func (r Results) Fail(err error) Results {
	r.failure = err.Error()
	return r
}

func (r Results) CursorUp() Results {
	return r.moveCursor(-1)
}

func (r Results) CursorDown() Results {
	return r.moveCursor(1)
}

// AtLastRow reports whether the cursor sits on the final loaded row, the
// point where scrolling further has to fetch another page.
func (r Results) AtLastRow() bool {
	return len(r.rows) > 0 && r.cursor == len(r.rows)-1
}

func (r Results) ScrollLeft() Results {
	r.firstCol = max(r.firstCol-1, 0)
	return r
}

func (r Results) ScrollRight() Results {
	r.firstCol = min(r.firstCol+1, max(len(r.columns)-1, 0))
	return r
}

// SelectedDocument returns the raw item behind the row under the cursor.
func (r Results) SelectedDocument() (json.RawMessage, bool) {
	if r.cursor >= len(r.raw) {
		return nil, false
	}
	return r.raw[r.cursor], true
}

func (r Results) Fetched() adapter.Page {
	return adapter.Page{Columns: r.columns, Rows: r.rows, Raw: r.raw}
}

func (r Results) View() string {
	return r.frame.render(r.content())
}

func (r Results) moveCursor(delta int) Results {
	if len(r.rows) == 0 {
		return r
	}
	r.cursor = min(max(r.cursor+delta, 0), len(r.rows)-1)
	return r
}

func (r Results) content() string {
	if len(r.columns) == 0 {
		return r.emptyContent()
	}
	return r.table()
}

func (r Results) emptyContent() string {
	switch {
	case r.failure != "" || r.banner != "":
		return strings.Join(append(r.bannerLines(), r.failureLines()...), "\n")
	case r.loaded:
		return theme.HintStyle().Render(noRowsHint)
	default:
		return theme.HintStyle().Render(resultsHint)
	}
}

// table renders the header and only the rows that fit under it. View runs on
// every frame, so a result set of thousands of rows must not be styled in
// full to show twenty of them.
//
// A failure is held to half the pane: the rows beneath it are the reason a
// failed fetch keeps them on screen at all.
func (r Results) table() string {
	width, height := r.frame.inner()
	columns := r.visibleColumns(width)
	widths := r.shownWidths(columns, width)
	lines := window(append(r.bannerLines(), r.failureLines()...), 0, max(height/2, 1))
	lines = append(lines, r.headerLine(columns, widths))
	start, end := windowBounds(len(r.rows), r.cursor, height-len(lines))
	for i := start; i < end; i++ {
		lines = append(lines, r.rowLine(r.rows[i], columns, widths, r.rowStyle(i)))
	}
	return strings.Join(lines, "\n")
}

// bannerLines precede the header with a blank line, so the sentence reads
// apart from the table under it.
func (r Results) bannerLines() []string {
	if r.banner == "" {
		return nil
	}
	width, _ := r.frame.inner()
	style := theme.TextStyle()
	if r.bannerFailed {
		style = theme.ErrorStyle()
	}
	return append(styleAll(style, wrapText(r.banner, width)), "")
}

func (r Results) rowStyle(i int) lipgloss.Style {
	switch i {
	case r.cursor:
		return theme.SelectedStyle()
	case r.failedRow:
		return theme.ErrorStyle()
	}
	return theme.TextStyle()
}

// failureLines renders the last failure wrapped rather than truncated, so the
// part of a service message that explains it cannot be cut off.
func (r Results) failureLines() []string {
	if r.failure == "" {
		return nil
	}
	width, _ := r.frame.inner()
	return styleAll(theme.ErrorStyle(), wrapText(r.failure, width))
}

// visibleColumns lists the columns that fit in width, starting at the
// horizontal scroll offset. One column is always listed, so a column wider
// than the pane can still be read truncated.
func (r Results) visibleColumns(width int) []int {
	var columns []int
	used := 0
	for i := r.firstCol; i < len(r.columns); i++ {
		next := used + r.cappedWidth(i)
		if len(columns) > 0 {
			next += len(columnGap)
		}
		if next > width && len(columns) > 0 {
			break
		}
		columns = append(columns, i)
		used = next
	}
	return columns
}

func (r Results) cappedWidth(i int) int { return min(r.widths[i], maxColumnWidth) }

// shownWidths sizes the visible columns: each at its capped width, then
// the width left in the pane given to the ones the cap cut short, in order.
func (r Results) shownWidths(columns []int, width int) []int {
	shown := make([]int, len(columns))
	spare := width - len(columnGap)*(len(columns)-1)
	for n, i := range columns {
		shown[n] = r.cappedWidth(i)
		spare -= shown[n]
	}
	for n, i := range columns {
		grow := min(max(spare, 0), r.widths[i]-shown[n])
		shown[n] += grow
		spare -= grow
	}
	return shown
}

func (r Results) headerLine(columns, widths []int) string {
	cells := make([]string, 0, len(columns))
	for n, i := range columns {
		cells = append(cells, fit(r.columns[i], widths[n]))
	}
	return theme.HeadingStyle().Render(strings.Join(cells, columnGap))
}

func (r Results) rowLine(row []string, columns, widths []int, style lipgloss.Style) string {
	cells := make([]string, 0, len(columns))
	for n, i := range columns {
		cells = append(cells, fit(cell(row, i), widths[n]))
	}
	return style.Render(strings.Join(cells, columnGap))
}

func columnWidths(columns []string, rows [][]string) []int {
	widths := make([]int, len(columns))
	for i, name := range columns {
		widths[i] = lipgloss.Width(name)
	}
	for _, row := range rows {
		for i := range min(len(row), len(widths)) {
			widths[i] = max(widths[i], lipgloss.Width(row[i]))
		}
	}
	return widths
}

// cell reads column i of row, which a page fetched before that column existed
// does not carry.
func cell(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return row[i]
}

// fit truncates text to width and pads it back out, so cells line up under
// their headers.
func fit(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}
