package panes

import (
	"fmt"
	"maps"
	"slices"
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
	helpHint  = "? help"
	noScope   = "no scope"
	noAccount = "no account"
	moreHint  = " (+more)"
	// simulatedBadge marks a result merged client-side, so its summed charge
	// is never mistaken for one server-side query.
	simulatedBadge = "simulated (client-side)"
	// pending stands in for a statistic no run has produced yet.
	pending = "—"
)

// noticeLife is how long a notice stays on screen before it retires itself.
const noticeLife = 5 * time.Second

// NoticeExpiredMsg retires the notice it names.
type NoticeExpiredMsg struct {
	// Notice is the number the status bar gave the notice this retires.
	// Notices are numbered from one, in the order they were set.
	Notice int
}

// Progress is the state of the current query run as the status bar reports
// it. The zero value is the bar before anything has been run.
type Progress struct {
	Stats   adapter.Stats
	More    bool // another page is available
	Running bool // a fetch is in flight
	Loaded  bool // a page has arrived, so Stats are worth showing
	// Simulated marks a run merged client-side from several containers.
	Simulated bool
}

// StatusBar is the one-line footer: account, active scope, and the statistics
// of the current result set.
type StatusBar struct {
	icons    theme.IconSet
	spinner  spinner.Model
	account  string
	scope    []string
	progress Progress
	notice   string
	notices  int
	width    int
	spinning bool
}

func NewStatusBar(icons theme.IconSet, account string) StatusBar {
	return StatusBar{
		icons: icons,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
			spinner.WithStyle(theme.SpinnerStyle()),
		),
		account: account,
	}
}

func (s StatusBar) Update(msg tea.Msg) (StatusBar, tea.Cmd) {
	switch msg := msg.(type) {
	case NoticeExpiredMsg:
		return s.retireNotice(msg), nil
	case spinner.TickMsg:
		return s.animate(msg)
	}
	return s, nil
}

// retireNotice drops the notice msg was issued for. A notice already replaced
// by a newer one is left to that one's own expiry.
func (s StatusBar) retireNotice(msg NoticeExpiredMsg) StatusBar {
	if msg.Notice == s.notices {
		s.notice = ""
	}
	return s
}

// animate advances the running animation and stops it once the run settles,
// so an idle bar wakes the program no further.
func (s StatusBar) animate(tick spinner.TickMsg) (StatusBar, tea.Cmd) {
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

// SetAccount names the account the session is on; empty is none.
func (s StatusBar) SetAccount(account string) StatusBar {
	s.account = account
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

// SetNotice reports something that went well outside the run itself. The
// returned command retires it after a few seconds, so nothing is left
// claiming an outcome the screen has long moved past. An empty notice clears
// it at once and has nothing to wait for.
func (s StatusBar) SetNotice(notice string) (StatusBar, tea.Cmd) {
	s.notice = notice
	if notice == "" {
		return s, nil
	}
	s.notices++
	return s, expireNotice(s.notices)
}

func expireNotice(notice int) tea.Cmd {
	return tea.Tick(noticeLife, func(time.Time) tea.Msg {
		return NoticeExpiredMsg{Notice: notice}
	})
}

func (s StatusBar) View() string {
	if s.width <= 0 {
		return ""
	}
	left := s.left(leafChargesLabel)
	if s.gap(left) < 1 {
		left = s.left(leafCountLabel)
	}
	if s.gap(left) < 1 {
		return ansi.Truncate(left, s.width, "…")
	}
	return left + strings.Repeat(" ", s.gap(left)) + theme.HintStyle().Render(helpHint)
}

// left joins the fields, with the charge broken down by breakdown.
func (s StatusBar) left(breakdown func(map[string]float64) string) string {
	separator := theme.HintStyle().Render(" " + s.icons.Separator + " ")
	return strings.Join(s.fields(breakdown), separator)
}

func (s StatusBar) gap(left string) int {
	return s.width - lipgloss.Width(left) - lipgloss.Width(helpHint)
}

func (s StatusBar) fields(breakdown func(map[string]float64) string) []string {
	fields := []string{
		theme.TextStyle().Render(s.accountLabel()),
		theme.TextStyle().Render(s.scopeLabel()),
	}
	if s.progress.Simulated {
		fields = append(fields, theme.HintStyle().Render(simulatedBadge))
	}
	fields = append(fields,
		theme.TextStyle().Render(s.rowsLabel()),
		chargeStyle().Render(s.chargeLabel(breakdown)),
		theme.TextStyle().Render(s.elapsedLabel()),
	)
	if s.progress.Running {
		fields = append(fields, s.spinner.View())
	}
	if s.notice != "" {
		fields = append(fields, theme.SuccessStyle().Render(s.notice))
	}
	return fields
}

func (s StatusBar) accountLabel() string {
	if s.account == "" {
		return noAccount
	}
	return s.account
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

func (s StatusBar) chargeLabel(breakdown func(map[string]float64) string) string {
	if !s.progress.Loaded {
		return pending + " RU"
	}
	return fmt.Sprintf("%.2f RU", s.progress.Stats.RequestCharge) + breakdown(s.progress.Stats.LeafCharges)
}

// leafChargesLabel breaks a summed charge down by container.
func leafChargesLabel(charges map[string]float64) string {
	if len(charges) < 2 {
		return ""
	}
	var parts []string
	for _, leaf := range slices.Sorted(maps.Keys(charges)) {
		parts = append(parts, fmt.Sprintf("%s %.2f", leaf, charges[leaf]))
	}
	return " (" + strings.Join(parts, " + ") + ")"
}

// leafCountLabel folds the breakdown to the number of containers, for a bar
// too narrow to hold every one.
func leafCountLabel(charges map[string]float64) string {
	if len(charges) < 2 {
		return ""
	}
	return fmt.Sprintf(" (%d containers)", len(charges))
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
