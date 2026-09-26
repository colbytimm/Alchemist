package panes_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func squiggled(text string) string {
	return theme.CurlyUnderline.Render(text, theme.DiagnosticError())
}

func press(editor panes.Editor, keys ...tea.KeyMsg) panes.Editor {
	for _, key := range keys {
		editor, _ = editor.Update(key)
	}
	return editor
}

func TestATypedNameIsFlaggedOnlyOnceChecked(t *testing.T) {
	editor := press(focusedEditor(""), typed("SELECT * FORM c"))
	require.NotContains(t, editor.View(), squiggled("FORM"), "names wait for the check")

	editor = editor.Diagnose()

	assert.Contains(t, editor.View(), squiggled("FORM"))
}

func TestTextPutInWholeIsCheckedAtOnce(t *testing.T) {
	editor := focusedEditor("")
	checked := editor.Diagnoses()

	editor = editor.SetValue("SELECT * FORM c")

	assert.Contains(t, editor.View(), squiggled("FORM"))
	assert.Equal(t, checked+1, editor.Diagnoses())
}

func TestALexicalProblemIsFlaggedWithoutACheck(t *testing.T) {
	view := focusedEditor("SELECT * FROM c WHERE c.total # 5").View()

	assert.Contains(t, view, squiggled("#"))
}

func TestTheTokenBeingTypedIsNotJudgedUntilTheCursorLeavesIt(t *testing.T) {
	editor := press(focusedEditor(""), typed("SELECT * FORM")).Diagnose()
	require.NotContains(t, editor.View(), squiggled("FORM"))

	editor = press(editor, pressed(tea.KeyHome))

	assert.Contains(t, editor.View(), squiggled("FORM"))
}

func TestAnUnterminatedStringIsNotFlaggedWhileTypingInIt(t *testing.T) {
	editor := press(focusedEditor(""), typed(`SELECT * FROM c WHERE c.region = "west`))
	require.NotContains(t, editor.View(), squiggled(`"west`))

	editor = press(editor, pressed(tea.KeyHome))

	assert.Contains(t, editor.View(), squiggled(`"west`))
}

func TestAnEditMovesTheFlagsAfterItAndDropsTheOneItTouches(t *testing.T) {
	editor := focusedEditor("SELECT * FORM c WHERE CONTAIN(c.n, 'a')").Diagnose()
	require.Contains(t, editor.View(), squiggled("FORM"))
	require.Contains(t, editor.View(), squiggled("CONTAIN"))

	editor = press(editor, pressed(tea.KeyHome), pressed(tea.KeyRight), pressed(tea.KeyRight),
		pressed(tea.KeyRight), pressed(tea.KeyRight), pressed(tea.KeyRight), pressed(tea.KeyRight), pressed(tea.KeyRight),
		pressed(tea.KeyRight), pressed(tea.KeyRight), pressed(tea.KeyRight), typed("X"))

	view := editor.View()
	assert.NotContains(t, view, squiggled("F"), "FORM was edited, so it waits for the next check")
	assert.Contains(t, view, squiggled("CONTAIN"), "CONTAIN moved along with its text")
}

func TestTheHintLineShowsTheMessageOnlyWhileTheCursorIsOnTheFlag(t *testing.T) {
	editor := focusedEditor("SELECT * FORM c").Diagnose()
	message := "FORM is not a clause: did you mean FROM?"
	require.NotContains(t, plain(editor.View()), message, "the cursor is past c")

	editor = press(editor, pressed(tea.KeyLeft), pressed(tea.KeyLeft))
	assert.Contains(t, plain(editor.View()), message)

	editor = press(editor, pressed(tea.KeyHome))
	assert.NotContains(t, plain(editor.View()), message)
}

func TestTheHintLineIsNotShownUnfocused(t *testing.T) {
	editor := press(focusedEditor("SELECT * FORM c").Diagnose(), pressed(tea.KeyLeft), pressed(tea.KeyLeft))
	require.Contains(t, plain(editor.View()), "is not a clause")

	assert.NotContains(t, plain(editor.Blur().View()), "is not a clause")
}

func TestDiagnosticAtIsTheFlagUnderACursor(t *testing.T) {
	editor := focusedEditor("SELECT * FORM c").Diagnose()

	d, ok := editor.DiagnosticAt(10)
	require.True(t, ok)
	assert.Equal(t, 9, d.Start)
	_, ok = editor.DiagnosticAt(2)
	assert.False(t, ok)
}

func TestAPlainUnderlineIsDrawnWhenChosen(t *testing.T) {
	view := focusedEditor("").SetDiagnosticUnderline(theme.PlainUnderline).SetValue("SELECT * FORM c").Diagnose().View()

	assert.Contains(t, view, theme.PlainUnderline.Render("FORM", theme.DiagnosticError()))
}

func TestDiagnosticsOffFlagNothingAndNeverCheck(t *testing.T) {
	editor := focusedEditor("").SetDiagnosticUnderline(theme.NoUnderline)
	checked := editor.Diagnoses()

	editor = editor.SetValue(`SELECT * FORM c WHERE c.a # "x`).Diagnose()

	assert.NotContains(t, editor.View(), "\x1b[4")
	assert.Equal(t, checked, editor.Diagnoses())
}
