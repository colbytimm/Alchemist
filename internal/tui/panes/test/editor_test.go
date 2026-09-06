package panes_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// editorWidth is wide enough for the placeholder to render in full.
const editorWidth = 44

func typed(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

func TestEditorHintsTheScopeSyntaxWhileEmpty(t *testing.T) {
	editor := panes.NewEditor().SetSize(editorWidth, paneHeight)

	assert.Contains(t, plain(editor.View()), "SELECT * FROM db.container AS c")
	assert.Empty(t, editor.Value())
}

func TestEditorTakesTypedText(t *testing.T) {
	editor, _ := panes.NewEditor().SetSize(editorWidth, paneHeight).Focus().Update(typed("SELECT 1"))

	assert.Equal(t, "SELECT 1", editor.Value())
	assert.Contains(t, plain(editor.View()), "SELECT 1")
}

func TestABlurredEditorIgnoresTyping(t *testing.T) {
	editor, _ := panes.NewEditor().SetSize(editorWidth, paneHeight).Blur().Update(typed("SELECT 1"))

	assert.Empty(t, editor.Value(), "only the focused pane takes keys")
}
