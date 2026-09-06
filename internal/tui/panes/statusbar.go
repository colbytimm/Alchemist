package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	helpHint = "? help"
	noScope  = "no scope"
	moreHint = " (+more)"
	// pending stands in for a statistic no run has produced yet.
	pending = "—"
)

// Progress is the state of the current query run as the status bar reports
// it. The zero value is the bar before anything has been run.
type Progress struct {
	Stats   adapter.Stats
	More    bool // another page is available
	Running bool // a fetch is in flight
	Loaded  bool // a page has arrived, so Stats are worth showing
}

// StatusBar is the one-line footer: profile, active scope, and the statistics
// of the current result set.
type StatusBar struct {
	icons    theme.IconSet
	spinner  spinner.Model
	profile  string
	scope    []string
	progress Progress
	width    int
	spinning bool
}

func NewStatusBar(icons theme.IconSet, profile string) StatusBar {
	return StatusBar{
		icons: icons,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
			spinner.WithStyle(theme.SpinnerStyle()),
		),
		profile: profile,
	}
}

// Update advances the running animation and stops it once the run settles, so
// an idle bar wakes the program no further.
func (s StatusBar) Update(msg tea.Msg) (StatusBar, tea.Cmd) {
	tick, ok := msg.(spinner.TickMsg)
	if !ok {
		return s, nil
	}
	if !s.progress.Running {
		s.spinning = false
		return s, nil
	}
	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(tick)
	return s, cmd
}

func (s StatusBar) SetWidth(width int) StatusBar {
	s.width = width
	return s
}

func (s StatusBar) SetScope(scope []string) StatusBar {
	s.scope = scope
	return s
}

// SetProgress records what the current run has produced. The returned command
// starts the running animation, and is nil while one is already playing.
func (s StatusBar) SetProgress(progress Progress) (StatusBar, tea.Cmd) {
	s.progress = progress
	if !progress.Running || s.spinning {
		return s, nil
	}
	s.spinning = true
	return s, s.spinner.Tick
}

func (s StatusBar) View() string {
	if s.width <= 0 {
		return ""
	}
	separator := theme.HintStyle().Render(" " + s.icons.Separator + " ")
	left := strings.Join(s.fields(), separator)
	gap := s.width - lipgloss.Width(left) - lipgloss.Width(helpHint)
	if gap < 1 {
		return ansi.Truncate(left, s.width, "…")
	}
	return left + strings.Repeat(" ", gap) + theme.HintStyle().Render(helpHint)
}

func (s StatusBar) fields() []string {
	fields := []string{
		theme.TextStyle().Render(s.profile),
		theme.TextStyle().Render(s.scopeLabel()),
		theme.TextStyle().Render(s.rowsLabel()),
		chargeStyle().Render(s.chargeLabel()),
		theme.TextStyle().Render(s.elapsedLabel()),
	}
	if s.progress.Running {
		fields = append(fields, s.spinner.View())
	}
	return fields
}

func (s StatusBar) scopeLabel() string {
	if len(s.scope) == 0 {
		return noScope
	}
	return strings.Join(s.scope, ".")
}

func (s StatusBar) rowsLabel() string {
	if !s.progress.Loaded {
		return pending + " rows"
	}
	rows := fmt.Sprintf("%d rows", s.progress.Stats.RowCount)
	if s.progress.More {
		rows += moreHint
	}
	return rows
}

func (s StatusBar) chargeLabel() string {
	if !s.progress.Loaded {
		return pending + " RU"
	}
	return fmt.Sprintf("%.2f RU", s.progress.Stats.RequestCharge)
}

func (s StatusBar) elapsedLabel() string {
	if !s.progress.Loaded {
		return pending + " elapsed"
	}
	return s.progress.Stats.Elapsed.Truncate(time.Millisecond).String()
}

// chargeStyle colors the request charge, the one statistic Cosmos users read
// at a glance.
func chargeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Verdigris())
}
