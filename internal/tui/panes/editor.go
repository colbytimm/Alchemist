package panes

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	editorTitle = "Editor"
	// editorHint previews the scope syntax the editor accepts.
	editorHint = "SELECT * FROM db.container AS c ..."
)

// Editor is the query buffer. Like the textarea it wraps, its value receiver
// hides shared pointers, so a caller must keep every Editor it is handed.
type Editor struct {
	frame frame
	area  textarea.Model
}

func NewEditor() Editor {
	area := textarea.New()
	area.Placeholder = editorHint
	area.ShowLineNumbers = false
	area.FocusedStyle, area.BlurredStyle = editorStyles()
	// A static cursor stays visible without a blink timer waking the program
	// twice a second; only the blinking mode returns a command to drive.
	area.Cursor.SetMode(cursor.CursorStatic)
	return Editor{frame: frame{title: editorTitle}, area: area}
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
	innerWidth, innerHeight := e.frame.inner()
	e.area.SetWidth(innerWidth)
	e.area.SetHeight(innerHeight)
	return e
}

// Focus gives the buffer the keyboard. The textarea's own focus command only
// drives a blinking cursor, and this one is static.
func (e Editor) Focus() Editor {
	e.frame = e.frame.focus()
	e.area.Focus()
	return e
}

func (e Editor) Blur() Editor {
	e.frame = e.frame.blur()
	e.area.Blur()
	return e
}

func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	var cmd tea.Cmd
	e.area, cmd = e.area.Update(msg)
	return e, cmd
}

func (e Editor) Value() string {
	return e.area.Value()
}

func (e Editor) View() string {
	return e.frame.render(e.area.View())
}

// SetValue replaces the buffer, leaving the cursor at its end.
func (e Editor) SetValue(text string) Editor {
	e.area.SetValue(text)
	return e
}
