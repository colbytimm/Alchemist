package tui_test

import (
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// newDiagnosingModel is a loaded session whose editor flags what it
// recognizes as wrong, as a session does unless its profile says otherwise.
func newDiagnosingModel(t *testing.T) tea.Model {
	t.Helper()
	m := newSession(t, newConnection(t), tui.Options{Manage: managed, SampleFields: true})
	model, _ := settle(m, m.Init())
	return tall(model)
}

func squiggled(text string) string {
	return theme.CurlyUnderline.Render(text, theme.DiagnosticError())
}

// typeHolding sends each key and keeps the command it returned instead of
// running it, the way keys typed faster than a timer fires leave several
// pending at once.
func typeHolding(m tea.Model, keys ...tea.KeyMsg) (tea.Model, []tea.Cmd) {
	var held []tea.Cmd
	for _, key := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key)
		held = append(held, cmd)
	}
	return m, held
}

// answersTogether runs the held commands side by side, so their timers run
// out together rather than one after another, and returns each one's
// messages in the order the commands were held.
func answersTogether(held []tea.Cmd) [][]tea.Msg {
	answered := make([][]tea.Msg, len(held))
	var wg sync.WaitGroup
	for i, cmd := range held {
		wg.Go(func() { answered[i] = answers(cmd) })
	}
	wg.Wait()
	return answered
}

func deliver(m tea.Model, msgs ...tea.Msg) tea.Model {
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	return m
}

func runes(text string) []tea.KeyMsg {
	keys := make([]tea.KeyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, keyRune(r))
	}
	return keys
}

func TestAFocusedBufferIsHighlightedAsItIsTyped(t *testing.T) {
	m := typeQuery(t, newTallModel(t, newConnection(t)), "SELECT COUNT(1) FROM c WHERE c.a = 'x'")

	view := m.View()

	assert.Contains(t, view, theme.SyntaxKeyword().Render("SELECT"))
	assert.Contains(t, view, theme.SyntaxFunction().Render("COUNT"))
	assert.Contains(t, view, theme.SyntaxString().Render("'x'"))
}

func TestAMisspelledFunctionIsFlaggedAfterThePauseAndNotBefore(t *testing.T) {
	m, held := typeHolding(pressAll(t, newDiagnosingModel(t), keyRune('e')), keyText("SELECT * FROM c WHERE CONTAIN(c.name, 'A')"))
	require.NotContains(t, m.View(), squiggled("CONTAIN"), "not while typing")

	m = deliver(m, answersTogether(held)[0]...)

	assert.Contains(t, m.View(), squiggled("CONTAIN"))
}

func TestTheHintLineNamesTheFixOnlyWhileTheCursorIsOnTheFlag(t *testing.T) {
	m, held := typeHolding(pressAll(t, newDiagnosingModel(t), keyRune('e')), keyText("SELECT * FROM c WHERE CONTAIN(c.name, 'A')"))
	m = deliver(m, answersTogether(held)[0]...)
	message := "unknown function CONTAIN: did you mean CONTAINS?"
	require.NotContains(t, plain(m.View()), message, "the cursor is at the end of the line")

	m = pressAll(t, m, keyMsg(tea.KeyHome))
	for range len("SELECT * FROM c WHERE C") {
		m = pressAll(t, m, keyMsg(tea.KeyRight))
	}
	assert.Contains(t, plain(m.View()), message)

	m = pressAll(t, m, keyMsg(tea.KeyEnd))
	assert.NotContains(t, plain(m.View()), message)
}

func TestKeysTypedWithinThePauseAreCheckedOnceAfterTheLast(t *testing.T) {
	m, held := typeHolding(pressAll(t, newDiagnosingModel(t), keyRune('e')), runes("SELECT * FORM c")...)
	answered := answersTogether(held)
	require.Len(t, answered, len("SELECT * FORM c"))

	for _, msgs := range answered[:len(answered)-1] {
		m = deliver(m, msgs...)
	}
	require.NotContains(t, m.View(), squiggled("FORM"), "a check an edit superseded is dropped")

	m = deliver(m, answered[len(answered)-1]...)
	assert.Contains(t, m.View(), squiggled("FORM"), "the last one runs")
}

func TestDiagnosticsOffScheduleNoCheck(t *testing.T) {
	m := newSession(t, newConnection(t), tui.Options{Manage: managed, Diagnostics: theme.NoUnderline})
	m = pressAll(t, m, keyRune('e'))

	m, cmd := m.Update(keyText(`SELECT * FORM c WHERE c.a # "x`))

	require.Contains(t, plain(m.View()), "FORM c", "the text went to the buffer")
	assert.Nil(t, cmd)
	assert.NotContains(t, m.View(), "\x1b[4")
}
