package panes

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/theme"
)

const helpTitle = "Keys"

// Help is the keybinding overlay. Its content is generated from the keymap it
// is built with, so it can never list a stale binding.
type Help struct {
	frame frame
	model help.Model
	keys  help.KeyMap
}

func NewHelp(keys help.KeyMap) Help {
	model := help.New()
	model.ShowAll = true
	model.Styles = helpStyles()
	return Help{frame: frame{title: helpTitle, focused: true}, model: model, keys: keys}
}

// helpStyles replaces the bubble's near-invisible greys with the palette.
func helpStyles() help.Styles {
	keyStyle := lipgloss.NewStyle().Foreground(theme.Gold())
	return help.Styles{
		Ellipsis:       theme.HintStyle(),
		ShortKey:       keyStyle,
		ShortDesc:      theme.TextStyle(),
		ShortSeparator: theme.HintStyle(),
		FullKey:        keyStyle,
		FullDesc:       theme.TextStyle(),
		FullSeparator:  theme.HintStyle(),
	}
}

func (h Help) SetSize(width, height int) Help {
	h.frame = h.frame.size(width, height)
	h.model.Width, _ = h.frame.inner()
	return h
}

func (h Help) View() string {
	return h.frame.render(h.model.View(h.keys))
}
