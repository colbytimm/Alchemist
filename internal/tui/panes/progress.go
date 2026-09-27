package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	progressLabelWidth = 11
	progressColumn     = 26
	percentWidth       = 6
	notASnapshot       = "The source can change while this runs. This is a copy, not a snapshot."
)

type CloneEnd int

const (
	CloneRunning CloneEnd = iota
	CloneDone
	CloneStopped
	CloneFailed
)

// CloneStatus is everything the progress view shows. The counters describe
// the container in progress; the charges and the projection, the whole job.
type CloneStatus struct {
	Source, Target clone.Endpoint
	Phase          string
	// Containers lists every container of a database clone, and is empty
	// for a container clone. Current is the one in progress.
	Containers []CloneContainerRow
	Current    int
	Items      bool // items are copied, not the definition alone
	Written    int64
	Skipped    int64
	Estimate   adapter.SizeEstimate
	Rate       float64 // items a second, zero until a page has been timed
	ReadCharge float64
	// WriteCharge and Projected are request units; Projected is zero until
	// it can be worked out.
	WriteCharge float64
	Projected   float64
	Writers     int
	MaxWriters  int
	Throttles   int
	End         CloneEnd
	Err         error
	// LeftBehind says what a clone that ended short leaves on the target.
	LeftBehind string
	Warning    string
	CanDelete  bool
}

type CloneContainerRow struct {
	Name     string
	Done     bool
	Written  int64
	Skipped  int64
	Estimate adapter.SizeEstimate
	Charge   float64
}

// CloneKeys are the bindings the progress view shows in its hint line, each
// only where it applies.
type CloneKeys struct {
	Hide, Stop, Resume, Delete, Close key.Binding
}

type CloneProgress struct {
	frame  frame
	icons  theme.IconSet
	hints  help.Model
	keys   CloneKeys
	status CloneStatus
}

func NewCloneProgress(icons theme.IconSet, keys CloneKeys) CloneProgress {
	hints := help.New()
	hints.Styles = helpStyles()
	return CloneProgress{frame: frame{focused: true}, icons: icons, hints: hints, keys: keys}
}

func (p CloneProgress) SetSize(width, height int) CloneProgress {
	p.frame = p.frame.size(width, height)
	p.hints.Width, _ = p.frame.inner()
	return p
}

func (p CloneProgress) SetStatus(status CloneStatus) CloneProgress {
	p.status = status
	p.frame.title = "Clone · " + status.Source.String() + " → " + status.Target.String()
	return p
}

func (p CloneProgress) View() string {
	width, _ := p.frame.inner()
	s := p.status
	lines := []string{theme.TextStyle().Render(s.Phase)}
	if bar := p.bar(width); bar != "" {
		lines = append(lines, bar)
	}
	lines = append(lines, "")
	if len(s.Containers) > 0 {
		lines = append(lines, p.containerLines(width)...)
	}
	if s.Items {
		lines = append(lines, p.counterLines(width)...)
		lines = append(lines, "")
		lines = append(lines, styleAll(theme.HintStyle(), wrapText(notASnapshot, width))...)
	}
	lines = append(lines, p.endLines(width)...)
	return p.frame.renderWithHint(lines, p.hints.ShortHelpView(p.hintKeys()))
}

// bar is drawn only when the size is known and there are items to count:
// a percentage of a guess would be a lie.
func (p CloneProgress) bar(width int) string {
	s := p.status
	if !s.Items || !EstimateHolds(s.Written+s.Skipped, s.Estimate) || s.End == CloneDone {
		return ""
	}
	fraction := min(float64(s.Written+s.Skipped)/float64(s.Estimate.Items), 1)
	cells := max(width-percentWidth, 1)
	full := int(fraction * float64(cells))
	return theme.SuccessStyle().Render(strings.Repeat(p.icons.BarFull, full)) +
		theme.HintStyle().Render(strings.Repeat(p.icons.BarEmpty, cells-full)) +
		theme.TextStyle().Render(fmt.Sprintf(" %3d%%", int(fraction*100)))
}

func (p CloneProgress) containerLines(width int) []string {
	s := p.status
	head := fmt.Sprintf("%s → %s on %s", s.Source.Path[0], s.Target.Path[0], s.Target.Account)
	count := fmt.Sprintf("%d of %d containers", min(s.Current+1, len(s.Containers)), len(s.Containers))
	lines := []string{theme.TextStyle().Render(spread(head, count, width))}
	for i, row := range s.Containers {
		lines = append(lines, p.containerRow(i, row, width))
	}
	return append(lines, "")
}

func (p CloneProgress) containerRow(i int, row CloneContainerRow, width int) string {
	s := p.status
	switch {
	case row.Done:
		text := fmt.Sprintf("%s %s  %s items  skipped %s  %s RU", p.icons.Success, row.Name,
			FormatCount(row.Written), FormatCount(row.Skipped), FormatCharge(row.Charge))
		return theme.SuccessStyle().Render(ansi.Truncate(text, width, "…"))
	case i == s.Current && s.End != CloneDone:
		text := fmt.Sprintf("%s %s  %s", p.icons.Collapsed, row.Name, OfAbout(row.Written, row.Estimate))
		return theme.SelectedStyle().Render(ansi.Truncate(text, width, "…"))
	}
	return theme.HintStyle().Render(ansi.Truncate("  "+row.Name, width, "…"))
}

func (p CloneProgress) counterLines(width int) []string {
	s := p.status
	rate, left := "measuring…", ""
	if s.Rate > 0 {
		rate = fmt.Sprintf("%s items/s", FormatCount(int64(s.Rate+0.5)))
		left = timeLeft(s)
	}
	projectionLabel, projected := s.projection()
	throttled := ""
	if s.Throttles > 0 {
		throttled = "throttled " + times(s.Throttles)
	}
	rows := [][3]string{
		{"Items", OfAbout(s.Written, s.Estimate), "skipped " + FormatCount(s.Skipped)},
		{"Rate", rate, left},
		{"RU read", FormatCharge(s.ReadCharge) + " on " + s.Source.Account, ""},
		{"RU write", FormatCharge(s.WriteCharge) + " on " + s.Target.Account, ""},
		{projectionLabel, projected, ""},
		{"Writers", fmt.Sprintf("%d of %d", s.Writers, s.MaxWriters), throttled},
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		line := fit(r[0], progressLabelWidth) + fit(r[1], progressColumn) + r[2]
		lines = append(lines, theme.TextStyle().Render(ansi.Truncate(strings.TrimRight(line, " "), width, "…")))
	}
	return lines
}

// projection is what the whole clone is expected to cost, and once it is
// done what it cost.
func (s CloneStatus) projection() (label, value string) {
	switch {
	case s.End == CloneDone:
		return "Spent", FormatCharge(s.ReadCharge+s.WriteCharge) + " RU in total"
	case !EstimateHolds(s.Written+s.Skipped, s.Estimate):
		return "Projected", "none: size not known"
	case s.Projected > 0:
		return "Projected", "about " + FormatCount(int64(s.Projected+0.5)) + " RU in total"
	}
	return "Projected", "after the first page"
}

// timeLeft is what the rate and the estimate say is left, and nothing when
// the estimate is not known.
func timeLeft(s CloneStatus) string {
	if !EstimateHolds(s.Written+s.Skipped, s.Estimate) || s.End != CloneRunning {
		return ""
	}
	remaining := time.Duration(float64(s.Estimate.Items-s.Written-s.Skipped) / s.Rate * float64(time.Second))
	switch {
	case remaining <= 0:
		return ""
	case remaining < time.Second:
		return "under a second"
	}
	return "about " + formatWait(remaining)
}

// OfAbout is a count against its estimate, 12,400 of about 30,112, or the
// count alone once the estimate no longer holds.
func OfAbout(done int64, estimate adapter.SizeEstimate) string {
	if !EstimateHolds(done, estimate) {
		return FormatCount(done)
	}
	return FormatCount(done) + " of about " + FormatCount(estimate.Items)
}

// EstimateHolds reports whether estimate is worth measuring done against: it
// is known, counts something, and has not been overtaken by the copy.
func EstimateHolds(done int64, estimate adapter.SizeEstimate) bool {
	return estimate.Known && estimate.Items > 0 && done <= estimate.Items
}

func (p CloneProgress) endLines(width int) []string {
	s := p.status
	var lines []string
	if s.Warning != "" {
		lines = append(lines, "")
		lines = append(lines, styleAll(theme.WarningStyle(), wrapText(s.Warning, width))...)
	}
	if s.End == CloneStopped || s.End == CloneFailed {
		lines = append(lines, "")
		if s.End == CloneFailed && s.Err != nil {
			lines = append(lines, styleAll(theme.ErrorStyle(), wrapText(p.icons.Failure+" "+s.Err.Error(), width))...)
		}
		lines = append(lines, styleAll(theme.TextStyle(), wrapText(s.LeftBehind, width))...)
	}
	return lines
}

func (p CloneProgress) hintKeys() []key.Binding {
	switch p.status.End {
	case CloneRunning:
		return []key.Binding{p.keys.Hide, p.keys.Stop}
	case CloneDone:
		return []key.Binding{p.keys.Close}
	}
	if p.status.CanDelete {
		return []key.Binding{p.keys.Resume, p.keys.Delete, p.keys.Close}
	}
	return []key.Binding{p.keys.Resume, p.keys.Close}
}

func spread(left, right string, width int) string {
	gap := width - len([]rune(left)) - len([]rune(right))
	if gap < 1 {
		return ansi.Truncate(left+" "+right, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}
