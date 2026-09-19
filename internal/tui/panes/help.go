package panes

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	helpTitle     = "Keys"
	helpColumnGap = "    "
)

// HelpSection is one titled column of the overlay: the keys that work in
// the place its title names.
type HelpSection struct {
	Title string
	Keys  []key.Binding
}

// Help is the keybinding overlay. Its content is generated from the keymap it
// is built with, so it can never list a stale binding.
type Help struct {
	frame    frame
	model    help.Model
	sections []HelpSection
}

func NewHelp(sections []HelpSection) Help {
	model := help.New()
	model.ShowAll = true
	model.Styles = helpStyles()
	return Help{frame: frame{title: helpTitle, focused: true}, model: model, sections: sections}
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
	return h
}

// View renders each section through the bubble on its own: given every
// column at once it has no place for a title above them.
func (h Help) View() string {
	columns := make([]string, 0, 2*len(h.sections))
	for i, section := range h.sections {
		if i > 0 {
			columns = append(columns, helpColumnGap)
		}
		column := lipgloss.JoinVertical(lipgloss.Left,
			headerStyle().Render(section.Title),
			h.model.FullHelpView([][]key.Binding{section.Keys}),
		)
		columns = append(columns, column)
	}
	return h.frame.render(lipgloss.JoinHorizontal(lipgloss.Top, columns...))
}
