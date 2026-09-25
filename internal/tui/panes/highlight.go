package panes

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

// highlightRows colors the rows a blurred textarea rendered for value. The
// textarea keeps its own wrapping and scroll position and exposes neither, so
// the rows are matched back to the text they show rather than laid out again:
// a second layout would move text whenever the two disagreed. A view that
// cannot be matched is returned as it came.
func highlightRows(value, view string) string {
	rows := strings.Split(ansi.Strip(view), "\n")
	for i, row := range rows {
		rows[i] = strings.TrimPrefix(row, editorPrompt)
	}
	kinds := spanKinds(value)
	prompt := theme.HintStyle().Render(editorPrompt)
	for start := 0; start <= len(value); start++ {
		if !utf8.RuneStart(byteAt(value, start)) {
			continue
		}
		painter := rowPainter{value: value, kinds: kinds, prompt: prompt, position: start}
		if painted, ok := painter.paint(rows); ok {
			return strings.Join(painted, "\n")
		}
	}
	return view
}

// byteAt reads past the end as a rune start, so the end of value is itself
// a position rows can be matched from.
func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 0
	}
	return s[i]
}

func spanKinds(value string) []query.SpanKind {
	kinds := make([]query.SpanKind, len(value))
	for _, span := range query.Spans(value) {
		for i := span.Start; i < span.End; i++ {
			kinds[i] = span.Kind
		}
	}
	return kinds
}

// rowPainter walks value alongside the rows that display it.
type rowPainter struct {
	value    string
	kinds    []query.SpanKind
	prompt   string
	position int
}

// paint reports false when rows show something other than value from the
// painter's position on.
func (p *rowPainter) paint(rows []string) ([]string, bool) {
	var painted []string
	for _, row := range rows {
		line, ok := p.paintRow(row)
		if !ok {
			return nil, false
		}
		painted = append(painted, p.prompt+line)
	}
	return painted, true
}

func (p *rowPainter) paintRow(row string) (string, bool) {
	var line styledLine
	for _, r := range row {
		kind, ok := p.take(r)
		if !ok {
			return "", false
		}
		line.add(r, kind)
	}
	if byteAt(p.value, p.position) == '\n' {
		p.position++
	}
	return line.String(), true
}

// take consumes r from value. The textarea shows every kind of space as a
// plain one, and a space value does not account for is it padding the row
// out, which belongs to no span.
func (p *rowPainter) take(r rune) (query.SpanKind, bool) {
	next, size := utf8.DecodeRuneInString(p.value[p.position:])
	if size > 0 && (next == r || r == ' ' && next != '\n' && unicode.IsSpace(next)) {
		kind := p.kinds[p.position]
		p.position += size
		return kind, true
	}
	return 0, r == ' '
}

// styledLine renders each run of one kind in a single style.
type styledLine struct {
	rendered strings.Builder
	run      strings.Builder
	kind     query.SpanKind
}

func (l *styledLine) add(r rune, kind query.SpanKind) {
	if kind != l.kind {
		l.flush()
		l.kind = kind
	}
	l.run.WriteRune(r)
}

func (l *styledLine) flush() {
	if l.run.Len() == 0 {
		return
	}
	l.rendered.WriteString(spanStyle(l.kind).Render(l.run.String()))
	l.run.Reset()
}

func (l *styledLine) String() string {
	l.flush()
	return l.rendered.String()
}

func spanStyle(kind query.SpanKind) lipgloss.Style {
	switch kind {
	case query.SpanKeyword:
		return lipgloss.NewStyle().Foreground(theme.Amethyst())
	case query.SpanString:
		return lipgloss.NewStyle().Foreground(theme.Verdigris())
	case query.SpanNumber:
		return lipgloss.NewStyle().Foreground(theme.Copper())
	case query.SpanComment:
		return theme.HintStyle()
	}
	return theme.TextStyle()
}
