package panes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	helpHint = "? help"
	noScope  = "no scope"
	// pending marks a statistic that iteration 5 will fill in.
	pending = "—"
)

// StatusBar is the one-line footer: profile, active scope, and the query
// statistics iteration 5 starts reporting.
type StatusBar struct {
	icons   theme.IconSet
	profile string
	scope   []string
	width   int
}

func NewStatusBar(icons theme.IconSet, profile string) StatusBar {
	return StatusBar{icons: icons, profile: profile}
}

func (s StatusBar) SetWidth(width int) StatusBar {
	s.width = width
	return s
}

func (s StatusBar) SetScope(scope []string) StatusBar {
	s.scope = scope
	return s
}

func (s StatusBar) View() string {
	if s.width <= 0 {
		return ""
	}
	left := strings.Join(s.fields(), " "+s.icons.Separator+" ")
	gap := s.width - lipgloss.Width(left) - lipgloss.Width(helpHint)
	if gap < 1 {
		return theme.TextStyle().Render(ansi.Truncate(left, s.width, "…"))
	}
	return theme.TextStyle().Render(left) + strings.Repeat(" ", gap) + theme.HintStyle().Render(helpHint)
}

func (s StatusBar) fields() []string {
	return []string{
		s.profile,
		s.scopeLabel(),
		pending + " rows",
		pending + " RU",
		pending + " elapsed",
	}
}

func (s StatusBar) scopeLabel() string {
	if len(s.scope) == 0 {
		return noScope
	}
	return strings.Join(s.scope, ".")
}
