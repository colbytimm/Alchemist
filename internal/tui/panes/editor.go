package panes

import "github.com/colbytimm/alchemist/internal/theme"

const (
	editorTitle = "Editor"
	// editorHint previews the scope syntax the editor accepts once iteration 5
	// makes the pane writable.
	editorHint = "SELECT * FROM db.container AS c"
)

// Editor is the query buffer. It renders a hint until iteration 5 gives it a
// textarea.
type Editor struct {
	width   int
	height  int
	focused bool
}

func NewEditor() Editor {
	return Editor{}
}

func (e Editor) SetSize(width, height int) Editor {
	e.width, e.height = width, height
	return e
}

func (e Editor) Focus() Editor {
	e.focused = true
	return e
}

func (e Editor) Blur() Editor {
	e.focused = false
	return e
}

func (e Editor) View() string {
	return frame{title: editorTitle, width: e.width, height: e.height, focused: e.focused}.
		render(theme.HintStyle().Render(editorHint))
}
