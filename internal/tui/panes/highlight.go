package panes

import (
	"iter"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

// highlighter keeps what drawing the editor needs between frames: the
// analysis of the value it last saw, rebuilt only when the value changes,
// the offset the first row showed last frame, where matching starts, and the
// last frame itself. Editors copied from one another share it; every entry
// is keyed by what it was made for, so a copy never draws with another's.
type highlighter struct {
	value      string
	analysis   query.Analysis
	spans      []query.Span
	lexical    []query.Diagnostic
	lineStarts []int
	analyses   int
	firstRow   int
	palette    palette
	frame      drawnFrame
}

// drawnFrame is a whole pane as drawn for an editor's stamp, in the styles
// and under the terminal's profile and background at the time.
type drawnFrame struct {
	stamp   uint64
	styles  *theme.Styles
	profile termenv.Profile
	dark    bool
	view    string
}

// frameFor is the frame drawn for stamp in styles, if nothing it depends on
// has changed since: a spinner tick redraws the screen, not the editor.
func (h *highlighter) frameFor(stamp uint64, styles *theme.Styles) (string, bool) {
	f := h.frame
	if f.stamp != stamp || f.styles != styles || f.profile != lipgloss.ColorProfile() || f.dark != lipgloss.HasDarkBackground() {
		return "", false
	}
	return f.view, true
}

func (h *highlighter) keepFrame(stamp uint64, styles *theme.Styles, view string) {
	h.frame = drawnFrame{stamp: stamp, styles: styles, profile: lipgloss.ColorProfile(), dark: lipgloss.HasDarkBackground(), view: view}
}

// analyze brings the cache up to value. A frame with no edit costs the
// comparison, which is a pointer check while the value is the one stored.
func (h *highlighter) analyze(value string) {
	if h.analyses > 0 && h.value == value {
		return
	}
	h.value = value
	h.analysis = query.Analyze(value)
	h.spans = h.analysis.Spans()
	h.lexical = h.analysis.LexicalDiagnostics()
	h.lineStarts = lineStarts(value)
	h.analyses++
}

func lineStarts(value string) []int {
	starts := []int{0}
	for i := range len(value) {
		if value[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// palette is the escape codes of every class and of the squiggle, rendered
// once for the styles, color profile, background and underline setting they
// were made under.
type palette struct {
	styles    *theme.Styles
	profile   termenv.Profile
	dark      bool
	setting   theme.DiagnosticUnderline
	ready     bool
	kinds     map[query.SpanKind]theme.Sequences
	plain     theme.Sequences
	cursor    theme.Sequences
	underline theme.Sequences
}

var spanStyles = map[query.SpanKind]func(*theme.Styles) lipgloss.Style{
	query.SpanKeyword:     (*theme.Styles).SyntaxKeyword,
	query.SpanOperator:    (*theme.Styles).SyntaxOperator,
	query.SpanLiteral:     (*theme.Styles).SyntaxLiteral,
	query.SpanFunction:    (*theme.Styles).SyntaxFunction,
	query.SpanAlias:       (*theme.Styles).SyntaxAlias,
	query.SpanParameter:   (*theme.Styles).SyntaxParameter,
	query.SpanString:      (*theme.Styles).SyntaxString,
	query.SpanNumber:      (*theme.Styles).SyntaxNumber,
	query.SpanComment:     (*theme.Styles).SyntaxComment,
	query.SpanPunctuation: (*theme.Styles).SyntaxPunctuation,
}

// currentPalette re-renders only when the styles, the terminal's profile or
// background, or the underline setting are not the ones the palette was
// made for.
func (h *highlighter) currentPalette(styles *theme.Styles, cursor lipgloss.Style, setting theme.DiagnosticUnderline) palette {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	p := h.palette
	if p.ready && p.styles == styles && p.profile == profile && p.dark == dark && p.setting == setting {
		return p
	}
	kinds := make(map[query.SpanKind]theme.Sequences, len(spanStyles))
	for kind, style := range spanStyles {
		kinds[kind] = theme.SequencesOf(style(styles))
	}
	h.palette = palette{
		styles:    styles,
		profile:   profile,
		dark:      dark,
		setting:   setting,
		ready:     true,
		kinds:     kinds,
		plain:     theme.SequencesOf(styles.TextStyle()),
		cursor:    theme.SequencesOf(cursor.Inline(true).Reverse(true)),
		underline: setting.Sequences(styles.DiagnosticError()),
	}
	return h.palette
}

func (p palette) of(kind query.SpanKind) theme.Sequences {
	if sequences, ok := p.kinds[kind]; ok {
		return sequences
	}
	return p.plain
}

// frameRows are the rows a textarea rendered: as it drew them, as the text
// they show after the prompt, and the column of the one cell it drew as its
// cursor, or -1.
type frameRows struct {
	raw    []string
	text   []string
	cursor []int
}

// readRows strips what the textarea styled. Its text and prompt styles are
// empty, so the only escape codes left are its cursor's, and the cell drawn
// reversed is the cursor.
func readRows(view string) frameRows {
	raw := strings.Split(view, "\n")
	rows := frameRows{raw: raw, text: make([]string, len(raw)), cursor: make([]int, len(raw))}
	promptWidth := utf8.RuneCountInString(editorPrompt)
	for i, line := range raw {
		text, cursor := stripRow(line)
		if trimmed, ok := strings.CutPrefix(text, editorPrompt); ok {
			text, cursor = trimmed, cursor-promptWidth
		}
		rows.text[i], rows.cursor[i] = text, max(cursor, -1)
	}
	return rows
}

// stripRow returns row without its escape codes, and the rune column of the
// first cell printed while reverse video was on, or -1.
func stripRow(row string) (string, int) {
	var text strings.Builder
	text.Grow(len(row))
	reversed, cursor, column := false, -1, 0
	for i := 0; i < len(row); {
		if row[i] == '\x1b' {
			end, sgr := escapeEnd(row, i)
			if sgr {
				reversed = reverseAfter(row[i+2:end-1], reversed)
			}
			i = end
			continue
		}
		_, size := utf8.DecodeRuneInString(row[i:])
		if reversed && cursor < 0 {
			cursor = column
		}
		text.WriteString(row[i : i+size])
		column++
		i += size
	}
	return text.String(), cursor
}

// escapeEnd is the offset past the escape sequence at i, and whether it is
// SGR. A control sequence runs to its final byte; anything else is taken to
// be two bytes long.
func escapeEnd(row string, i int) (int, bool) {
	if i+1 >= len(row) || row[i+1] != '[' {
		return min(i+2, len(row)), false
	}
	for j := i + 2; j < len(row); j++ {
		if row[j] >= 0x40 && row[j] <= 0x7e {
			return j + 1, row[j] == 'm'
		}
	}
	return len(row), false
}

func reverseAfter(params string, reversed bool) bool {
	for _, param := range strings.Split(params, ";") {
		switch param {
		case "7":
			reversed = true
		case "", "0", "27":
			reversed = false
		}
	}
	return reversed
}

// paintRequest is one frame to paint: the textarea's view of value with the
// cursor on cursorLine, and the diagnostics on show.
type paintRequest struct {
	styles      *theme.Styles
	view        string
	cursorLine  int
	prompt      lipgloss.Style
	cursor      lipgloss.Style
	diagnostics []query.Diagnostic
	underline   theme.DiagnosticUnderline
}

// paint colors the rows the textarea rendered. The textarea keeps its own
// wrapping and scroll position and exposes neither, so the rows are matched
// back to the text they show rather than laid out again: a second layout
// would move text whenever the two disagreed. Matching tries the offset
// that matched last frame, then the lines that can be on screen with the
// cursor, then every rune within them, so a frame's work is bounded by the
// rows it shows. A view that cannot be matched keeps its text plain.
func (h *highlighter) paint(r paintRequest) string {
	rows := readRows(r.view)
	painter := rowPainter{
		highlighter: h,
		palette:     h.currentPalette(r.styles, r.cursor, r.underline),
		prompt:      r.prompt.Render(editorPrompt),
		diagnostics: r.diagnostics,
	}
	for start := range h.candidates(r.cursorLine, len(rows.text)) {
		if painted, ok := painter.paint(rows, start); ok {
			h.firstRow = start
			return painted
		}
	}
	return painter.promptsOnly(rows)
}

// candidates are the offsets the first row may start at, most likely first:
// where it started last frame, then the starts of the lines that can share
// the screen with the cursor's, which the first row lies at most height
// lines above, then every rune within those lines, for a first row that
// begins partway through a wrapped line. Only a textarea that scrolled its
// cursor out of sight gets past them, to every line start.
func (h *highlighter) candidates(cursorLine, height int) iter.Seq[int] {
	return func(yield func(int) bool) {
		if h.firstRow <= len(h.value) && utf8.RuneStart(byteAt(h.value, h.firstRow)) && !yield(h.firstRow) {
			return
		}
		last := min(cursorLine, len(h.lineStarts)-1)
		first := max(last-height, 0)
		for line := last; line >= first; line-- {
			if !yield(h.lineStarts[line]) {
				return
			}
		}
		for line := last; line >= first; line-- {
			if !h.yieldWithin(line, yield) {
				return
			}
		}
		for _, start := range h.lineStarts {
			if !yield(start) {
				return
			}
		}
	}
}

func (h *highlighter) yieldWithin(line int, yield func(int) bool) bool {
	end := len(h.value)
	if line+1 < len(h.lineStarts) {
		end = h.lineStarts[line+1] - 1
	}
	for offset := h.lineStarts[line] + 1; offset <= end; offset++ {
		if utf8.RuneStart(byteAt(h.value, offset)) && !yield(offset) {
			return false
		}
	}
	return true
}

// byteAt reads past the end as a rune start, so the end of value is itself
// a position rows can be matched from.
func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

// rowPainter walks the value alongside the rows that display it.
type rowPainter struct {
	*highlighter
	palette     palette
	prompt      string
	diagnostics []query.Diagnostic
}

// paint reports false when the rows show something other than the value
// from start on.
func (p rowPainter) paint(rows frameRows, start int) (string, bool) {
	walk := valueWalk{
		value:       p.value,
		position:    start,
		spans:       p.spans[firstEndingAfter(p.spans, start, spanEnd):],
		diagnostics: p.diagnostics[firstEndingAfter(p.diagnostics, start, diagnosticEnd):],
	}
	var out strings.Builder
	out.Grow(len(rows.text) * 64)
	for i, row := range rows.text {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(p.prompt)
		if !p.paintRow(&out, &walk, row, rows.cursor[i]) {
			return "", false
		}
		walk.endRow()
	}
	return out.String(), true
}

func (p rowPainter) paintRow(out *strings.Builder, walk *valueWalk, row string, cursor int) bool {
	var run strings.Builder
	var runStyle runeStyle
	flush := func() {
		if run.Len() > 0 {
			p.writeRun(out, runStyle, run.String())
			run.Reset()
		}
	}
	column := 0
	for _, r := range row {
		c, ok := walk.take(r)
		if !ok {
			return false
		}
		c.cursor = column == cursor
		if c != runStyle {
			flush()
			runStyle = c
		}
		run.WriteRune(r)
		column++
	}
	flush()
	return true
}

func (p rowPainter) writeRun(out *strings.Builder, c runeStyle, text string) {
	if c.cursor {
		out.WriteString(p.palette.cursor.Open + text + p.palette.cursor.Close)
		return
	}
	sequences := p.palette.of(c.kind)
	out.WriteString(sequences.Open)
	if c.diagnosed {
		out.WriteString(p.palette.underline.Open)
		out.WriteString(text)
		out.WriteString(p.palette.underline.Close)
	} else {
		out.WriteString(text)
	}
	out.WriteString(sequences.Close)
}

// promptsOnly styles the prompts of a view that does not show the value,
// the placeholder's, and leaves the rest as the textarea drew it.
func (p rowPainter) promptsOnly(rows frameRows) string {
	painted := make([]string, len(rows.raw))
	for i, raw := range rows.raw {
		if rest, ok := strings.CutPrefix(raw, editorPrompt); ok {
			raw = p.prompt + rest
		}
		painted[i] = raw
	}
	return strings.Join(painted, "\n")
}

// runeStyle is how one rune is drawn.
type runeStyle struct {
	kind      query.SpanKind
	diagnosed bool
	cursor    bool
}

// valueWalk is a position in the value, with the spans and diagnostics not
// yet passed.
type valueWalk struct {
	value       string
	position    int
	spans       []query.Span
	diagnostics []query.Diagnostic
}

// take consumes r from the value. The textarea shows every kind of space as
// a plain one, and a space the value does not account for is padding out
// the row, which belongs to no span.
func (w *valueWalk) take(r rune) (runeStyle, bool) {
	next, size := utf8.DecodeRuneInString(w.value[w.position:])
	if size == 0 || next != r && (r != ' ' || next == '\n' || !unicode.IsSpace(next)) {
		return runeStyle{}, r == ' '
	}
	c := runeStyle{kind: w.kindAt(), diagnosed: w.diagnosedAt()}
	w.position += size
	return c, true
}

func (w *valueWalk) kindAt() query.SpanKind {
	for len(w.spans) > 0 && w.spans[0].End <= w.position {
		w.spans = w.spans[1:]
	}
	if len(w.spans) > 0 && w.spans[0].Start <= w.position {
		return w.spans[0].Kind
	}
	return 0
}

func (w *valueWalk) diagnosedAt() bool {
	for len(w.diagnostics) > 0 && w.diagnostics[0].End <= w.position {
		w.diagnostics = w.diagnostics[1:]
	}
	return len(w.diagnostics) > 0 && w.diagnostics[0].Start <= w.position
}

func (w *valueWalk) endRow() {
	if byteAt(w.value, w.position) == '\n' {
		w.position++
	}
}

// firstEndingAfter is the index of the first of ranges, in order, that ends
// past offset.
func firstEndingAfter[T any](ranges []T, offset int, end func(T) int) int {
	return sort.Search(len(ranges), func(i int) bool { return end(ranges[i]) > offset })
}

func spanEnd(s query.Span) int { return s.End }

func diagnosticEnd(d query.Diagnostic) int { return d.End }
