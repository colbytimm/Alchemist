package panes

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

// previewQuery uses every syntax class, and a function the check flags.
const previewQuery = `SELECT o.id, UPPER(o.status) AS s  -- open only
FROM sales.orders o
WHERE o.total > @minTotal AND o.paid = true
  AND CONTAIN(o.tags, 'rush') AND o.qty >= 2.5`

const noColorNote = "colors are off (NO_COLOR)"

// previewMargin is the blank column either side of the sample.
const previewMargin = 1

// previewMinRows are the lines a preview keeps however little room it has:
// the query, a results header and row, and the status bar.
const previewMinRows = 7

// previewLine is one line of the preview. The lines with the highest drop
// go first when the preview has too little room; zero is never dropped.
type previewLine struct {
	text string
	drop int
}

const (
	keep = iota
	dropLate
	dropSoon
	dropFirst
)

// previewBox draws the sample in styles, on the theme's own background
// when it names one, in a focused frame so the accent role shows as it
// does on the workspace.
func (p ThemePicker) previewBox(styles *theme.Styles, width, height int) string {
	box := frame{title: previewTitle(styles.Theme()), width: width, height: height, focused: true}
	innerWidth, innerHeight := box.inner()
	lines := fitPreview(p.previewLines(styles, innerWidth-2*previewMargin), innerHeight)
	margin := strings.Repeat(" ", previewMargin)
	for i, line := range lines {
		lines[i] = margin + line
	}
	return box.renderIn(styles, strings.Join(onBackground(styles, padBody(lines, innerHeight), innerWidth), "\n"))
}

func previewTitle(t theme.Theme) string {
	if title := t.About().Title; title != "" {
		return title
	}
	return t.Name()
}

func (p ThemePicker) previewLines(styles *theme.Styles, width int) []previewLine {
	text, hint := styles.TextStyle(), styles.HintStyle()
	var lines []previewLine
	add := func(drop int, line string) {
		lines = append(lines, previewLine{text: clipLine(line, width), drop: drop})
	}
	add(dropSoon, hint.Render(credit(styles.Theme())))
	if lipgloss.ColorProfile() == termenv.Ascii {
		add(dropLate, hint.Render(noColorNote))
	}
	add(dropFirst, "")
	query, diagnostic := highlightSample(styles, previewQuery)
	for _, line := range strings.Split(query, "\n") {
		add(keep, line)
	}
	add(dropSoon, styles.ErrorStyle().Render(diagnostic))
	add(dropFirst, "")
	add(keep, styles.HeadingStyle().Render("id     status    total"))
	add(keep, styles.SelectedStyle().Render("o003   open      57.5"))
	add(dropSoon, text.Render("o004   shipped   70"))
	add(dropFirst, "")
	add(dropSoon, strings.Join([]string{
		styles.SuccessStyle().Render("+ added"),
		styles.ErrorStyle().Render(p.icons.Removed + " removed"),
		styles.WarningStyle().Render("! warning"),
		styles.ErrorStyle().Render(p.icons.Failure + " failed"),
	}, "   "))
	separator := hint.Render(" " + p.icons.Separator + " ")
	add(keep, strings.Join([]string{
		text.Render("emulator"), text.Render("sales.orders"), text.Render("2 rows"), styles.SuccessStyle().Render("1.00 RU"),
	}, separator))
	return lines
}

// credit names the theme's author and license, and whether it has colors
// of its own for light terminals.
func credit(t theme.Theme) string {
	about := t.About()
	parts := slices.DeleteFunc([]string{about.Author, about.License}, func(s string) bool { return s == "" })
	if t.Adapts() {
		parts = append(parts, "adapts to light and dark")
	} else {
		parts = append(parts, "dark only")
	}
	return strings.Join(parts, " · ")
}

// fitPreview drops lines, soonest first and from the bottom, until the
// rest fit height.
func fitPreview(lines []previewLine, height int) []string {
	for len(lines) > height {
		worst := len(lines) - 1
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i].drop > lines[worst].drop {
				worst = i
			}
		}
		if lines[worst].drop == keep {
			break
		}
		lines = slices.Delete(lines, worst, worst+1)
	}
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.text)
	}
	return texts
}

// highlightSample paints text the way the editor paints its buffer, with
// the check's diagnostics underlined, and returns the first diagnostic's
// message as the editor's hint line would show it.
func highlightSample(styles *theme.Styles, text string) (string, string) {
	h := &highlighter{}
	h.analyze(text)
	diagnostics := query.Diagnose(h.analysis)
	slices.SortFunc(diagnostics, func(a, b query.Diagnostic) int { return a.Start - b.Start })
	lines := strings.Split(text, "\n")
	cursor := slices.Repeat([]int{-1}, len(lines))
	painter := rowPainter{
		highlighter: h,
		palette:     h.currentPalette(styles, lipgloss.NewStyle(), theme.CurlyUnderline),
		diagnostics: diagnostics,
	}
	painted, ok := painter.paint(frameRows{text: lines, cursor: cursor}, 0)
	if !ok {
		painted = text
	}
	message := ""
	if len(diagnostics) > 0 {
		message = diagnostics[0].Message
	}
	return painted, message
}

// onBackground paints the theme's background behind every line, padded to
// width. Each styled run ends in a full reset, so the background is opened
// again after every one.
func onBackground(styles *theme.Styles, lines []string, width int) []string {
	background, ok := styles.BackgroundStyle()
	if !ok {
		return lines
	}
	sequences := theme.SequencesOf(background)
	if sequences.Open == "" {
		return lines
	}
	painted := make([]string, len(lines))
	for i, line := range lines {
		line += strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
		painted[i] = sequences.Open + strings.ReplaceAll(line, resetSequence, resetSequence+sequences.Open) + sequences.Close
	}
	return painted
}

const resetSequence = "\x1b[0m"
