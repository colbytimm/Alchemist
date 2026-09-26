package panes

import (
	"slices"

	"github.com/colbytimm/alchemist/internal/query"
	"github.com/colbytimm/alchemist/internal/theme"
)

// notTyping marks a cursor that has moved off the last edit.
const notTyping = -1

// marks are the editor's diagnostics: those the last Diagnose found, moved
// along by every edit since, and the edit the cursor still sits at, whose
// token is not judged until the cursor leaves it.
type marks struct {
	underline theme.DiagnosticUnderline
	checked   []query.Diagnostic
	typingAt  int
	runs      int
	hinting   bool
}

// edited moves the checked ranges past an edit of before into after. An
// insertion before a range moves it; an edit touching a range drops it, as
// the next Diagnose will judge the token again.
func (m marks) edited(before, after string, cursor int) marks {
	start, oldEnd, newEnd := changedRange(before, after)
	delta := newEnd - oldEnd
	insertion := start == oldEnd
	var kept []query.Diagnostic
	for _, d := range m.checked {
		insertedBefore := insertion && d.Start == start
		switch {
		case d.End < start:
			kept = append(kept, d)
		case insertedBefore || d.Start > oldEnd:
			d.Start, d.End = d.Start+delta, d.End+delta
			kept = append(kept, d)
		}
	}
	m.checked, m.typingAt = kept, cursor
	return m
}

// changedRange is where before and after differ: from start to oldEnd in
// before, and to newEnd in after.
func changedRange(before, after string) (start, oldEnd, newEnd int) {
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	oldEnd, newEnd = len(before), len(after)
	for oldEnd > start && newEnd > start && before[oldEnd-1] == after[newEnd-1] {
		oldEnd--
		newEnd--
	}
	return start, oldEnd, newEnd
}

// shown are the diagnostics drawn: the lexical ones of the buffer as it is,
// and the checked ones they do not overlap, less any at the token being
// typed.
func (m marks) shown(lexical []query.Diagnostic) []query.Diagnostic {
	if m.underline == theme.NoUnderline {
		return nil
	}
	var shown []query.Diagnostic
	for _, d := range lexical {
		if !m.typingIn(d) {
			shown = append(shown, d)
		}
	}
	for _, d := range m.checked {
		if !m.typingIn(d) && !slices.ContainsFunc(lexical, d.Overlaps) {
			shown = append(shown, d)
		}
	}
	slices.SortFunc(shown, func(a, b query.Diagnostic) int { return a.Start - b.Start })
	return shown
}

func (m marks) typingIn(d query.Diagnostic) bool {
	return d.Start <= m.typingAt && m.typingAt <= d.End
}

func (e Editor) shownDiagnostics() []query.Diagnostic {
	e.highlight.analyze(e.value)
	return e.marks.shown(e.highlight.lexical)
}

// Diagnose checks the buffer, which the root model does once typing pauses.
func (e Editor) Diagnose() Editor {
	if e.marks.underline == theme.NoUnderline {
		return e
	}
	e.highlight.analyze(e.value)
	e.marks.checked = query.Diagnose(e.highlight.analysis)
	e.marks.runs++
	return e.settleHint().restamp()
}

// Diagnoses counts the times the buffer has been checked.
func (e Editor) Diagnoses() int {
	return e.marks.runs
}

// Analyses counts the times the buffer has been lexed and parsed for
// painting, which only an edit should cause.
func (e Editor) Analyses() int {
	return e.highlight.analyses
}

// DiagnosticAt is the diagnostic shown at cursor, a byte offset of the
// buffer; a cursor just past a range is still at it.
func (e Editor) DiagnosticAt(cursor int) (query.Diagnostic, bool) {
	for _, d := range e.shownDiagnostics() {
		if d.Start <= cursor && cursor <= d.End {
			return d, true
		}
	}
	return query.Diagnostic{}, false
}

// settleHint gives the hint line a row while the focused cursor is on a
// diagnostic, and takes it back after.
func (e Editor) settleHint() Editor {
	_, onDiagnostic := e.DiagnosticAt(e.cursorIn(e.value))
	hinting := onDiagnostic && e.frame.focused
	if hinting == e.marks.hinting {
		return e
	}
	e.marks.hinting = hinting
	return e.layout()
}

func (e Editor) hintLine(width int) string {
	d, _ := e.DiagnosticAt(e.cursorIn(e.value))
	return theme.ErrorStyle().Render(clipLine(d.Message, width))
}
