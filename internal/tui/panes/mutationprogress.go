package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/mutate"
	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

const stayWritten = "Items already %s stay %s if this stops."

type MutationEnd int

const (
	MutationRunning MutationEnd = iota
	MutationDone
	MutationStopped
	MutationFailed
)

// MutationStatus is everything the progress view shows of a job.
type MutationStatus struct {
	Account   string
	Container []string
	Kind      query.MutationKind
	Progress  mutate.Progress
	// Rate is items a second, zero until a chunk has been timed;
	// Projected is the request units the whole job is expected to cost,
	// zero until anything has been written.
	Rate       float64
	Projected  float64
	MaxWriters int
	Stopping   bool
	End        MutationEnd
	Err        error
	Warning    string
}

// MutationKeys are the bindings the progress view shows in its hint line,
// each only where it applies.
type MutationKeys struct {
	Hide, Stop, Resume, Report key.Binding
}

type MutationProgress struct {
	frame  frame
	icons  theme.IconSet
	hints  help.Model
	keys   MutationKeys
	status MutationStatus
}

func NewMutationProgress(icons theme.IconSet, keys MutationKeys) MutationProgress {
	hints := help.New()
	return MutationProgress{frame: frame{focused: true}, icons: icons, hints: hints, keys: keys}
}

func (p MutationProgress) SetSize(width, height int) MutationProgress {
	p.frame = p.frame.size(width, height)
	p.hints.Width, _ = p.frame.inner()
	return p
}

func (p MutationProgress) SetStatus(status MutationStatus) MutationProgress {
	p.status = status
	p.frame.title = fmt.Sprintf("%s · %s / %s", Capitalized(status.Kind.String()), status.Account, strings.Join(status.Container, "."))
	return p
}

func (p MutationProgress) View() string {
	width, _ := p.frame.inner()
	s := p.status
	lines := []string{theme.TextStyle().Render(p.phase())}
	if s.End == MutationRunning {
		lines = append(lines, p.bar(width))
	}
	lines = append(lines, "")
	lines = append(lines, p.counterLines(width)...)
	lines = append(lines, "")
	applied := s.Kind.Applied()
	lines = append(lines, styleAll(theme.HintStyle(), wrapText(fmt.Sprintf(stayWritten, applied, applied), width))...)
	lines = append(lines, p.endLines(width)...)
	return p.frame.renderWithHint(lines, themedHelp(p.hints).ShortHelpView(p.hintKeys()))
}

func (p MutationProgress) phase() string {
	s := p.status
	switch {
	case s.End == MutationDone:
		return "Done."
	case s.End == MutationStopped:
		return "Stopped."
	case s.End == MutationFailed:
		return "Failed."
	case s.Stopping:
		return "Stopping after the writes in flight…"
	}
	return Capitalized(s.Kind.Ongoing()) + " items"
}

func (p MutationProgress) bar(width int) string {
	progress := p.status.Progress
	fraction := 1.0
	if progress.Total > 0 {
		fraction = float64(progress.Total-progress.Counts.NotAttempted) / float64(progress.Total)
	}
	cells := max(width-percentWidth, 1)
	full := int(fraction * float64(cells))
	return theme.SuccessStyle().Render(strings.Repeat(p.icons.BarFull, full)) +
		theme.HintStyle().Render(strings.Repeat(p.icons.BarEmpty, cells-full)) +
		theme.TextStyle().Render(fmt.Sprintf(" %3d%%", int(fraction*100)))
}

func (p MutationProgress) counterLines(width int) []string {
	s := p.status
	c := s.Progress.Counts
	done := int64(s.Progress.Total - c.NotAttempted)
	rate, left := "measuring…", ""
	if s.Rate > 0 {
		rate = FormatCount(int64(s.Rate+0.5)) + " items/s"
		left = p.timeLeft()
	}
	throttled := ""
	if s.Progress.Throttles > 0 {
		throttled = "throttled " + times(s.Progress.Throttles)
	}
	spent, projected := p.charges()
	rows := [][3]string{
		{"Items", FormatCount(done) + " of " + FormatCount(int64(s.Progress.Total)), ""},
		{Capitalized(s.Kind.Applied()), FormatCount(int64(c.Applied)), skippedText(c)},
		{"Failed", FormatCount(int64(c.Failed)), "unknown " + FormatCount(int64(c.Unknown))},
		{"Rate", rate, left},
		{"RU", spent, projected},
		{"Writers", fmt.Sprintf("%d of %d", s.Progress.Writers, s.MaxWriters), throttled},
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		line := fit(r[0], progressLabelWidth) + fit(r[1], progressColumn) + r[2]
		lines = append(lines, theme.TextStyle().Render(ansi.Truncate(strings.TrimRight(line, " "), width, "…")))
	}
	return lines
}

func skippedText(c mutate.Counts) string {
	text := "skipped " + FormatCount(int64(c.Skipped()))
	var reasons []string
	for _, reason := range []struct {
		n    int
		name string
	}{{c.Changed, "changed"}, {c.Gone, "gone"}, {c.NoKey, "no key"}, {c.NoParent, "no parent"}} {
		if reason.n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d %s", reason.n, reason.name))
		}
	}
	if len(reasons) > 0 {
		text += " (" + strings.Join(reasons, ", ") + ")"
	}
	return text
}

// charges are what the writes have spent, and what the whole job is
// expected to spend once anything has been written.
func (p MutationProgress) charges() (spent, projected string) {
	s := p.status
	switch {
	case s.End != MutationRunning:
		return FormatCharge(s.Progress.WriteCharge) + " spent on writes", ""
	case s.Projected > 0:
		projected = "about " + FormatCount(int64(s.Projected+0.5)) + " in total"
	case s.Progress.Counts.Attempted() > 0:
		projected = noCharges
	}
	return FormatCharge(s.Progress.WriteCharge) + " so far", projected
}

func (p MutationProgress) timeLeft() string {
	s := p.status
	if s.End != MutationRunning {
		return ""
	}
	remaining := time.Duration(float64(s.Progress.Counts.NotAttempted) / s.Rate * float64(time.Second))
	switch {
	case remaining <= 0:
		return ""
	case remaining < time.Second:
		return "under a second"
	}
	return "about " + formatWait(remaining)
}

// endLines say what a job that is over did, and for one that ended short,
// what it left as it was.
func (p MutationProgress) endLines(width int) []string {
	s := p.status
	var lines []string
	if s.Warning != "" {
		lines = append(lines, "")
		lines = append(lines, styleAll(theme.WarningStyle(), wrapText(s.Warning, width))...)
	}
	if s.End == MutationRunning {
		return lines
	}
	lines = append(lines, "")
	if s.End == MutationFailed && s.Err != nil {
		lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(p.icons.Failure+" "+s.Err.Error(), width))...)
	}
	return append(lines, styleAll(theme.TextStyle(), wrapText(p.outcomeText(), width))...)
}

func (p MutationProgress) outcomeText() string {
	s := p.status
	c := s.Progress.Counts
	outcomes := fmt.Sprintf("%s %s, %s skipped, %s failed, %s unknown", count(c.Applied), s.Kind.Applied(),
		count(c.Skipped()), count(c.Failed), count(c.Unknown))
	if c.NotAttempted == 0 {
		return "Every item has an outcome: " + outcomes + "."
	}
	return fmt.Sprintf("%s of %s items were attempted: %s. %s were not attempted and are unchanged.",
		count(c.Attempted()), count(s.Progress.Total), outcomes, count(c.NotAttempted))
}

func count(n int) string {
	return FormatCount(int64(n))
}

func (p MutationProgress) hintKeys() []key.Binding {
	switch p.status.End {
	case MutationRunning:
		return []key.Binding{p.keys.Hide, p.keys.Stop}
	case MutationDone:
		return []key.Binding{p.keys.Report}
	}
	return []key.Binding{p.keys.Resume, p.keys.Report}
}

func Capitalized(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
