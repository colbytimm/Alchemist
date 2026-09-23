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

// View lays the sections out as columns, wrapping onto a further row of
// columns where the pane is too narrow for them all side by side.
func (h Help) View() string {
	width, _ := h.frame.inner()
	var rows, row []string
	rowWidth := 0
	for _, section := range h.sections {
		column := h.column(section)
		columnWidth := lipgloss.Width(column)
		if len(row) > 0 && rowWidth+len(helpColumnGap)+columnWidth > width {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...), "")
			row, rowWidth = nil, 0
		}
		if len(row) > 0 {
			row = append(row, helpColumnGap)
			rowWidth += len(helpColumnGap)
		}
		row = append(row, column)
		rowWidth += columnWidth
	}
	rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	return h.frame.render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// column renders one section through the bubble on its own: given every
// column at once it has no place for a title above them.
func (h Help) column(section HelpSection) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle().Render(section.Title),
		h.model.FullHelpView([][]key.Binding{section.Keys}),
	)
}
