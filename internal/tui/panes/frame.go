// Package panes holds the individual TUI panes. A pane owns its own state and
// rendering; layout, focus, and every adapter call belong to the root model in
// internal/tui.
package panes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/theme"
)

// A frame smaller than this has no room for a border and a title.
const (
	minFrameWidth  = 8
	minFrameHeight = 3
)

// frame is the chrome every pane draws: a rounded border occupying exactly
// width by height cells, with title written into the top edge.
type frame struct {
	title   string
	width   int
	height  int
	focused bool
}

// render draws content inside f, clipping whatever does not fit.
func (f frame) render(content string) string {
	if f.width < minFrameWidth || f.height < minFrameHeight {
		return ""
	}
	border := f.borderStyle()
	inner := lipgloss.NewStyle().
		MaxWidth(f.width - 2).
		MaxHeight(f.height - 2).
		Render(content)
	body := border.
		BorderTop(false).
		Width(f.width - 2).
		Height(f.height - 2).
		Render(inner)
	edge := lipgloss.NewStyle().Foreground(border.GetBorderTopForeground())
	return edge.Render(f.topEdge()) + "\n" + body
}

func (f frame) borderStyle() lipgloss.Style {
	if f.focused {
		return theme.FocusedBorderStyle()
	}
	return theme.BlurredBorderStyle()
}

// topEdge builds the border's top line: both corners, one leading dash, the
// title, then dashes to the far corner.
func (f frame) topEdge() string {
	border, _, _, _, _ := f.borderStyle().GetBorder()
	available := f.width - 3
	label := ansi.Truncate(" "+f.title+" ", available, "…")
	return border.TopLeft + border.Top + label +
		strings.Repeat(border.Top, available-lipgloss.Width(label)) + border.TopRight
}
