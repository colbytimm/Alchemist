// Package panes holds the individual TUI panes. A pane owns its own state and
// rendering; layout, focus, and every adapter call belong to the root model in
// internal/tui.
package panes

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/theme"
)

// A frame smaller than this has no room for a border and a title.
const (
	minFrameWidth  = 8
	minFrameHeight = 3
)

// borderCells is the width and the height the border itself consumes.
const borderCells = 2

// wrapText breaks text into lines of at most width cells, so a message
// wraps rather than being cut off where it stops fitting.
func wrapText(text string, width int) []string {
	wrapped := lipgloss.NewStyle().Width(max(width, 1)).Render(text)
	var lines []string
	for _, line := range strings.Split(wrapped, "\n") {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// failureLines wraps a failure to fit, and is empty when there is none.
func failureLines(failure string, width int) []string {
	if failure == "" {
		return nil
	}
	return wrapText(failure, width)
}

// clampScroll keeps offset within the count lines a body of height can
// scroll through: never above the first, never past the last.
func clampScroll(offset, count, height int) int {
	return min(max(offset, 0), max(count-height, 0))
}

// accountTitle names the account a pane's content belongs to beside its
// title.
func accountTitle(title, account string) string {
	if account == "" {
		return title
	}
	return title + " · " + account
}

func styleAll(style lipgloss.Style, lines []string) []string {
	styled := make([]string, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, style.Render(line))
	}
	return styled
}

// frame is the chrome every pane draws: a rounded border occupying exactly
// width by height cells, with title written into the top edge.
type frame struct {
	title   string
	width   int
	height  int
	focused bool
}

func (f frame) size(width, height int) frame {
	f.width, f.height = width, height
	return f
}

func (f frame) focus() frame {
	f.focused = true
	return f
}

func (f frame) blur() frame {
	f.focused = false
	return f
}

// inner is the content box left once the border is drawn.
func (f frame) inner() (width, height int) {
	return max(f.width-borderCells, 0), max(f.height-borderCells, 0)
}

// render draws content inside f, clipping whatever does not fit.
func (f frame) render(content string) string {
	if f.width < minFrameWidth || f.height < minFrameHeight {
		return ""
	}
	width, height := f.inner()
	border := f.borderStyle()
	clipped := lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(content)
	body := border.BorderTop(false).Width(width).Height(height).Render(clipped)
	edge := lipgloss.NewStyle().Foreground(border.GetBorderTopForeground())
	return edge.Render(f.topEdge()) + "\n" + body
}

// renderWithHint draws lines inside f, padded so hint lands on its last line.
func (f frame) renderWithHint(lines []string, hint string) string {
	return f.renderWithHints(lines, []string{hint})
}

// renderWithHints draws lines inside f, padded so hints land on its last
// lines.
func (f frame) renderWithHints(lines, hints []string) string {
	_, height := f.inner()
	body := make([]string, max(height-len(hints), len(lines)))
	copy(body, lines)
	return f.render(strings.Join(append(body, hints...), "\n"))
}

// packHints packs the enabled keys into as few lines of width as they
// take, so none is cut off at the edge of a narrow terminal.
func packHints(hints help.Model, keys []key.Binding, width int) []string {
	hints.Width = 0
	var lines []string
	var line []key.Binding
	for _, binding := range keys {
		if !binding.Enabled() {
			continue
		}
		candidate := append(slices.Clone(line), binding)
		if len(line) > 0 && lipgloss.Width(hints.ShortHelpView(candidate)) > width {
			lines = append(lines, hints.ShortHelpView(line))
			candidate = []key.Binding{binding}
		}
		line = candidate
	}
	if len(line) > 0 {
		lines = append(lines, hints.ShortHelpView(line))
	}
	return lines
}

func (f frame) borderStyle() lipgloss.Style {
	if f.focused {
		return theme.FocusedBorderStyle()
	}
	return theme.BlurredBorderStyle()
}

// topEdge draws the title into the top border, which lipgloss cannot do: it
// only renders a border of uniform runes.
func (f frame) topEdge() string {
	border, _, _, _, _ := f.borderStyle().GetBorder()
	available := f.width - borderCells - 1 // the corners, plus one leading dash
	label := ansi.Truncate(" "+f.title+" ", available, "…")
	return border.TopLeft + border.Top + label +
		strings.Repeat(border.Top, available-lipgloss.Width(label)) + border.TopRight
}
