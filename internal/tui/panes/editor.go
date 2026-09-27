package panes

import (
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/complete"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	editorTitle = "Editor"
	// editorHint previews the scope syntax the editor accepts.
	editorHint   = "SELECT * FROM db.container AS c ..."
	editorPrompt = "┃ "
)

// Editor is the query buffer with the completion list docked under it. Like
// the textarea it wraps, its value receiver hides shared pointers, so a
// caller must keep every Editor it is handed.
type Editor struct {
	frame       frame
	area        textarea.Model
	suggestions Suggestions
	// value is the textarea's, read once per change rather than rebuilt
	// from its lines on every call.
	value     string
	highlight *highlighter
	marks     marks
	// stamp names what the editor shows: every change that can alter a
	// frame takes a new one, and a frame drawn for a stamp is reused.
	stamp uint64
}

// stamps are unique across every editor, so two copies of one that changed
// apart can never share a stamp and so a frame.
var stamps atomic.Uint64

func (e Editor) restamp() Editor {
	e.stamp = stamps.Add(1)
	return e
}

// NewEditor builds the buffer; accept is the binding the list's hint line
// names for taking a suggestion.
func NewEditor(accept key.Binding) Editor {
	area := textarea.New()
	area.Placeholder = editorHint
	area.Prompt = editorPrompt
	area.ShowLineNumbers = false
	// The bubble stops enter at 99 lines by default, short of a batch of the
	// service's 100 operations written one to a line.
	area.MaxHeight = 0
	area.FocusedStyle = bufferStyle(theme.HintStyle())
	area.BlurredStyle = area.FocusedStyle
	// A static cursor stays visible without a blink timer waking the program
	// twice a second; only the blinking mode returns a command to drive.
	area.Cursor.SetMode(cursor.CursorStatic)
	return Editor{
		frame:       frame{title: editorTitle},
		area:        area,
		suggestions: newSuggestions(accept),
		highlight:   &highlighter{},
		marks:       marks{typingAt: notTyping},
	}.restamp()
}

// bufferStyle leaves the buffer's text and prompt unstyled: the highlighter
// paints every row the textarea renders, and an empty style costs the
// textarea nothing per line where a colored one costs a render. Only the
// placeholder, which the highlighter leaves alone, keeps a color.
func bufferStyle(placeholder lipgloss.Style) textarea.Style {
	return textarea.Style{
		Base:        lipgloss.NewStyle(),
		CursorLine:  lipgloss.NewStyle(),
		EndOfBuffer: lipgloss.NewStyle(),
		Placeholder: placeholder,
		Prompt:      lipgloss.NewStyle(),
		Text:        lipgloss.NewStyle(),
	}
}

// themedArea is the textarea with its placeholder in styles. The textarea
// draws through a pointer to whichever of its styles was current when it
// was last focused or blurred, which a copy shares with the original, so
// the copy is pointed at its own.
func (e Editor) themedArea(styles *theme.Styles) textarea.Model {
	area := e.area
	area.FocusedStyle = bufferStyle(styles.HintStyle())
	area.BlurredStyle = area.FocusedStyle
	if area.Focused() {
		_ = area.Focus() // a static cursor has no blink to drive
	} else {
		area.Blur()
	}
	return area
}

// SetDiagnosticUnderline chooses how flagged ranges are drawn; NoUnderline
// also stops them being looked for.
func (e Editor) SetDiagnosticUnderline(underline theme.DiagnosticUnderline) Editor {
	e.marks.underline = underline
	e.marks.checked = nil
	return e.settleHint().restamp()
}

func (e Editor) SetSize(width, height int) Editor {
	e.frame = e.frame.size(width, height)
	innerWidth, _ := e.frame.inner()
	e.area.SetWidth(innerWidth)
	return e.layout()
}

// layout gives the buffer the rows the list leaves it, then lays it out
// again from the top: the textarea's viewport only ever scrolls to chase the
// cursor, so a buffer that shrank would hide the cursor line and one that
// grew back would keep rows scrolled away above blank ones. Rebuilding the
// value resets that scroll; the cursor is walked back the way Replace does.
// The textarea cannot be shorter than one row, so a pane with room for the
// list alone still renders a buffer row, which View drops.
func (e Editor) layout() Editor {
	e.area.SetHeight(max(e.bufferRows(), 1))
	_, offset := e.Cursor()
	return e.Replace(offset, offset, "").scrollToCursor().restamp()
}

// scrollToCursor brings a cursor below the window back on screen. The
// textarea only scrolls at the end of its Update, only while focused, and
// only through lines a View has handed its viewport, so a blurred one is
// focused for the length of an empty update, after a frame nobody shows.
func (e Editor) scrollToCursor() Editor {
	focused := e.area.Focused()
	if !focused {
		e.area.Focus()
	}
	_ = e.area.View()
	e.area, _ = e.area.Update(nil)
	if !focused {
		e.area.Blur()
	}
	return e
}

func (e Editor) bufferRows() int {
	_, inner := e.frame.inner()
	return inner - e.suggestions.rows(inner) - e.hintRows(inner)
}

func (e Editor) hintRows(inner int) int {
	if !e.marks.hinting || e.suggestions.Open() || inner < 2 {
		return 0
	}
	return 1
}

// Focus gives the buffer the keyboard. The textarea's own focus command only
// drives a blinking cursor, and this one is static.
func (e Editor) Focus() Editor {
	e.frame = e.frame.focus()
	e.area.Focus()
	return e.settleHint().restamp()
}

// Blur takes the keyboard away and the list with it: a list can only be
// answered from the buffer it belongs to.
func (e Editor) Blur() Editor {
	e.frame = e.frame.blur()
	e.area.Blur()
	e.suggestions = e.suggestions.Clear()
	return e.settleHint().layout()
}

// Update scrolls after a key that takes the cursor to another row, which may
// lie past the rows the textarea's viewport last saw and could scroll to.
func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	var cmd tea.Cmd
	row := e.cursorRow()
	e.area, cmd = e.area.Update(msg)
	if e.cursorRow() != row {
		e = e.scrollToCursor()
	}
	return e.settle(), cmd
}

// cursorRow places the cursor among the rows the buffer wraps into.
type cursorRow struct {
	line, wrapped, lines int
}

func (e Editor) cursorRow() cursorRow {
	return cursorRow{line: e.area.Line(), wrapped: e.area.LineInfo().RowOffset, lines: e.area.LineCount()}
}

// settle catches up with a textarea that may have been edited, or had its
// cursor moved, since the editor last looked.
func (e Editor) settle() Editor {
	value := e.area.Value()
	cursor := e.cursorIn(value)
	switch {
	case value != e.value:
		e.marks = e.marks.edited(e.value, value, cursor)
		e.value = value
	case cursor != e.marks.typingAt:
		e.marks.typingAt = notTyping
	}
	return e.settleHint().restamp()
}

func (e Editor) Value() string {
	return e.value
}

// View paints the rows the textarea rendered. The list, when open, takes the
// bottom rows of the pane; otherwise the diagnostic under the cursor may
// take the last.
func (e Editor) View() string {
	styles := theme.Active()
	if frame, ok := e.highlight.frameFor(e.stamp, styles); ok {
		return frame
	}
	frame := e.draw(styles)
	e.highlight.keepFrame(e.stamp, styles, frame)
	return frame
}

func (e Editor) draw(styles *theme.Styles) string {
	view := e.paint(styles, e.themedArea(styles).View())
	width, inner := e.frame.inner()
	switch {
	case e.suggestions.Open():
		buffer := e.bufferLines(view)
		lines := slices.Concat(buffer, e.suggestions.lines(width, inner-len(buffer)))
		return e.frame.render(strings.Join(lines, "\n"))
	case e.hintRows(inner) > 0:
		lines := append(e.bufferLines(view), e.hintLine(width))
		return e.frame.render(strings.Join(lines, "\n"))
	}
	return e.frame.render(view)
}

func (e Editor) bufferLines(view string) []string {
	buffer := strings.Split(view, "\n")
	return buffer[:min(len(buffer), e.bufferRows())]
}

func (e Editor) paint(styles *theme.Styles, view string) string {
	e.highlight.analyze(e.value)
	prompt := styles.HintStyle()
	if e.frame.focused {
		prompt = styles.AccentStyle()
	}
	return e.highlight.paint(paintRequest{
		styles:      styles,
		view:        view,
		cursorLine:  e.area.Line(),
		prompt:      prompt,
		cursor:      e.area.Cursor.Style,
		diagnostics: e.shownDiagnostics(),
		underline:   e.marks.underline,
	})
}

// SetValue replaces the buffer, leaving the cursor at its end, and checks it
// at once: text put there whole is not being typed.
func (e Editor) SetValue(text string) Editor {
	e.area.SetValue(text)
	e = e.settle()
	e.marks.typingAt = notTyping
	return e.Diagnose()
}

// Context is what can be typed at the cursor, read from the same analysis
// of the buffer that paints it.
func (e Editor) Context() query.Completion {
	e.highlight.analyze(e.value)
	return e.highlight.analysis.Context(e.cursorIn(e.value))
}

// Cursor is the buffer and the byte offset of the cursor within it.
func (e Editor) Cursor() (string, int) {
	return e.value, e.cursorIn(e.value)
}

func (e Editor) cursorIn(text string) int {
	info := e.area.LineInfo()
	return byteOffset(text, e.area.Line(), info.StartColumn+info.ColumnOffset)
}

// byteOffset locates the col'th rune of line row in text.
func byteOffset(text string, row, col int) int {
	offset := 0
	for range row {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return len(text)
		}
		offset += next + 1
	}
	for range col {
		if offset == len(text) || text[offset] == '\n' {
			break
		}
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}
	return offset
}

// Replace puts with in place of the bytes from start to end and leaves the
// cursor after it, checking the buffer at once as SetValue does. The
// textarea has no way to delete a range, so the buffer is rebuilt and the
// cursor walked back up to where the text now ends.
func (e Editor) Replace(start, end int, with string) Editor {
	text := e.value
	start = min(max(start, 0), len(text))
	end = min(max(end, start), len(text))
	e.area.SetValue(text[:start] + with + text[end:])
	e = e.moveCursorTo(start + len(with)).settle()
	if e.value == text {
		return e
	}
	return e.Diagnose()
}

func (e Editor) moveCursorTo(offset int) Editor {
	before := e.area.Value()[:offset]
	row := strings.Count(before, "\n")
	col := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:])
	for e.area.Line() > row {
		e.area.CursorUp()
	}
	e.area.SetCursor(col)
	return e
}

// SetSuggestions docks the list with items, and note in its hint line while
// something is still being fetched for it. Nothing to show closes the list.
func (e Editor) SetSuggestions(items []complete.Suggestion, note string) Editor {
	e.suggestions = e.suggestions.Set(items, note)
	return e.layout()
}

// RefreshSuggestions is SetSuggestions for rows that arrived on their own,
// which leaves a chosen row chosen.
func (e Editor) RefreshSuggestions(items []complete.Suggestion, note string) Editor {
	e.suggestions = e.suggestions.Refresh(items, note)
	return e.layout()
}

// ClearSuggestions closes the list. Closing a list that is not open leaves
// the layout alone, which spares every keystroke that opens none a new layout.
func (e Editor) ClearSuggestions() Editor {
	if !e.suggestions.Open() {
		return e
	}
	e.suggestions = e.suggestions.Clear()
	return e.layout()
}

// Suggesting reports whether a suggestion is on offer to accept or choose.
func (e Editor) Suggesting() bool {
	_, ok := e.suggestions.Selected()
	return ok
}

func (e Editor) NextSuggestion() Editor {
	e.suggestions = e.suggestions.CursorDown()
	return e.restamp()
}

func (e Editor) PrevSuggestion() Editor {
	e.suggestions = e.suggestions.CursorUp()
	return e.restamp()
}

func (e Editor) Selected() (complete.Suggestion, bool) {
	return e.suggestions.Selected()
}
