package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// burst is a word arriving as one message, as a paste the terminal did not
// bracket, or input faster than it reports keys, delivers it.
func burst(word string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(word)}
}

// keyNames are words that are also the names of keys some binding matches.
var keyNames = []string{
	"left", "right", "up", "down", "home", "end", "tab", "shift+tab",
	"enter", "esc", "backspace", "delete", "ctrl+c", "ctrl+r", "ctrl+e", "ctrl+s",
}

func TestAWordArrivingInOneBurstIsTypedIntoTheEditor(t *testing.T) {
	for _, name := range keyNames {
		t.Run(name, func(t *testing.T) {
			m := typeQuery(t, newLoadedModel(t, newConnection(t)), "SELECT * FROM a.b AS ")

			m = pressAll(t, m, burst(name), keyText(" ON x"))

			assert.Contains(t, plain(m.View()), "AS "+name+" ON x")
		})
	}
}

func TestAWordArrivingInOneBurstIsTypedIntoAPrompt(t *testing.T) {
	for _, name := range []string{"left", "home", "esc", "enter"} {
		t.Run(name, func(t *testing.T) {
			m := pressAll(t, openExport(t, newResultsModel(t)), keyMsg(tea.KeyCtrlU), burst(name))

			view := plain(m.View())
			assert.Contains(t, view, exportTitle, "the prompt is still open")
			assert.Contains(t, view, name)
		})
	}
}

func TestAWordArrivingInOneBurstIsTypedIntoTheConnectForm(t *testing.T) {
	for _, name := range []string{"left", "tab", "enter", "esc"} {
		t.Run(name, func(t *testing.T) {
			m := newConnectModel(t, &connector{t: t}, panes.ConnectForm{})

			m = pressAll(t, m, burst(name))

			view := plain(m.View())
			assert.Contains(t, view, "Profile   "+name)
		})
	}
}
