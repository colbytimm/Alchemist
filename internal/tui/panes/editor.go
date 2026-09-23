package panes

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/complete"
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
}

// NewEditor builds the buffer; accept is the binding the list's hint line
// names for taking a suggestion.
func NewEditor(accept key.Binding) Editor {
	area := textarea.New()
	area.Placeholder = editorHint
	area.Prompt = editorPrompt
	area.ShowLineNumbers = false
	area.FocusedStyle, area.BlurredStyle = editorStyles()
	// A static cursor stays visible without a blink timer waking the program
	// twice a second; only the blinking mode returns a command to drive.
	area.Cursor.SetMode(cursor.CursorStatic)
	return Editor{frame: frame{title: editorTitle}, area: area, suggestions: newSuggestions(accept)}
}

// editorStyles replaces the bubble's fixed greys with the palette, and drops
// the cursor-line highlight the frame border already delimits.
func editorStyles() (focused, blurred textarea.Style) {
	blurred = textarea.Style{
		Base:        lipgloss.NewStyle(),
		CursorLine:  lipgloss.NewStyle(),
		EndOfBuffer: lipgloss.NewStyle(),
		Placeholder: theme.HintStyle(),
		Prompt:      theme.HintStyle(),
		Text:        theme.TextStyle(),
	}
	focused = blurred
	focused.Prompt = lipgloss.NewStyle().Foreground(theme.Gold())
	return focused, blurred
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
	return e.Replace(offset, offset, "").scrollToCursor()
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
	return inner - e.suggestions.rows(inner)
}

// Focus gives the buffer the keyboard. The textarea's own focus command only
// drives a blinking cursor, and this one is static.
func (e Editor) Focus() Editor {
	e.frame = e.frame.focus()
	e.area.Focus()
	return e
}

// Blur takes the keyboard away and the list with it: a list can only be
// answered from the buffer it belongs to.
func (e Editor) Blur() Editor {
	e.frame = e.frame.blur()
	e.area.Blur()
	return e.ClearSuggestions()
}

func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	var cmd tea.Cmd
	e.area, cmd = e.area.Update(msg)
	return e, cmd
}

func (e Editor) Value() string {
	return e.area.Value()
}

// View shows the buffer itself while it has the keyboard, since a textarea
// cannot style a region of editable text, and the same rows colored once it
// does not. The list, when open, takes the bottom rows of the pane.
func (e Editor) View() string {
	view := e.area.View()
	if !e.frame.focused {
		view = highlightRows(e.area.Value(), view)
	}
	if !e.suggestions.Open() {
		return e.frame.render(view)
	}
	width, inner := e.frame.inner()
	buffer := strings.Split(view, "\n")
	buffer = buffer[:min(len(buffer), e.bufferRows())]
	lines := slices.Concat(buffer, e.suggestions.lines(width, inner-len(buffer)))
	return e.frame.render(strings.Join(lines, "\n"))
}

// SetValue replaces the buffer, leaving the cursor at its end.
func (e Editor) SetValue(text string) Editor {
	e.area.SetValue(text)
	return e
}

// Cursor is the buffer and the byte offset of the cursor within it.
func (e Editor) Cursor() (string, int) {
	text := e.area.Value()
	info := e.area.LineInfo()
	return text, byteOffset(text, e.area.Line(), info.StartColumn+info.ColumnOffset)
}

// byteOffset locates the col'th rune of line row in text.
func byteOffset(text string, row, col int) int {
	offset := 0
	for i, line := range strings.Split(text, "\n") {
		if i == row {
			runes := []rune(line)
			return offset + len(string(runes[:min(col, len(runes))]))
		}
		offset += len(line) + 1
	}
	return len(text)
}

// Replace puts with in place of the bytes from start to end and leaves the
// cursor after it. The textarea has no way to delete a range, so the buffer
// is rebuilt and the cursor walked back up to where the text now ends.
func (e Editor) Replace(start, end int, with string) Editor {
	text := e.area.Value()
	start = min(max(start, 0), len(text))
	end = min(max(end, start), len(text))
	e.area.SetValue(text[:start] + with + text[end:])
	return e.moveCursorTo(start + len(with))
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

func (e Editor) ClearSuggestions() Editor {
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
	return e
}

func (e Editor) PrevSuggestion() Editor {
	e.suggestions = e.suggestions.CursorUp()
	return e
}

func (e Editor) Selected() (complete.Suggestion, bool) {
	return e.suggestions.Selected()
}
