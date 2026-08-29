package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

func TestQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		m := tui.New(theme.Icons())
		_, cmd := m.Update(key)
		require.NotNil(t, cmd, "key %q should produce a command", key.String())
		assert.IsType(t, tea.QuitMsg{}, cmd(), "key %q should quit", key.String())
	}
}

func TestOtherKeysDoNotQuit(t *testing.T) {
	m := tui.New(theme.Icons())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Nil(t, cmd)
}

func TestViewContainsLogoAndHint(t *testing.T) {
	m := tui.New(theme.Icons())
	view := m.View()
	assert.Contains(t, view, `/_\`, "view should contain the ASCII logo")
	assert.Contains(t, view, "press q to quit")
}

func TestWindowResizeCentersContent(t *testing.T) {
	m := tui.New(theme.Icons())
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	assert.Nil(t, cmd)
	view := updated.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, `/_\`)
}
