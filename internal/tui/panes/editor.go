package panes

import "github.com/colbytimm/alchemist/internal/theme"

const (
	editorTitle = "Editor"
	// editorHint previews the scope syntax the editor accepts once iteration 5
	// makes the pane writable.
	editorHint = "SELECT * FROM db.container AS c"
)

// Editor is the query buffer, a hint until iteration 5 gives it a textarea.
type Editor struct {
	frame frame
}

func NewEditor() Editor {
	return Editor{frame: frame{title: editorTitle}}
}

func (e Editor) SetSize(width, height int) Editor {
	e.frame = e.frame.size(width, height)
	return e
}

func (e Editor) Focus() Editor {
	e.frame = e.frame.focus()
	return e
}

func (e Editor) Blur() Editor {
	e.frame = e.frame.blur()
	return e
}

func (e Editor) View() string {
	return e.frame.render(theme.HintStyle().Render(editorHint))
}
