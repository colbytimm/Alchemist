package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	itemDiffTitle = "Item"
	// diffContext is how many unchanged lines stay on either side of a
	// change; longer runs fold into one line that counts them.
	diffContext = 3
	// itemChrome is the footer line and the hint line.
	itemChrome = 2
)

// ItemDiff is one changed item, or a definition, as a line diff on
// Detail's frame and scrolling.
type ItemDiff struct {
	frame  frame
	icons  theme.IconSet
	hints  help.Model
	keys   []key.Binding
	lines  []string
	footer string
	offset int
}

func NewItemDiff(icons theme.IconSet, keys []key.Binding) ItemDiff {
	hints := help.New()
	return ItemDiff{frame: frame{title: itemDiffTitle, focused: true}, icons: icons, hints: hints, keys: keys}
}

func (d ItemDiff) SetSize(width, height int) ItemDiff {
	d.frame = d.frame.size(width, height)
	width, _ = d.frame.inner()
	d.hints.Width = width
	return d
}

// SetItem shows the change of one item: the line diff of its two bodies,
// or, past snapshot.MaxLineDiff lines, the structural list. An added or a
// removed item is its whole body in one color.
func (d ItemDiff) SetItem(item snapshot.ItemChange, before, after []byte) ItemDiff {
	d.frame.title = fmt.Sprintf("%s · %s / %s · %s", itemDiffTitle, item.Identity.PartitionKeyText(), item.Identity.ID, item.Kind)
	d.offset = 0
	lines, ok := snapshot.Lines(before, after)
	if ok {
		d.lines = d.render(lines)
	} else {
		d.lines = structuralLines(before, after)
	}
	d.footer = itemFooter(item, before, after)
	return d
}

// SetDefinition shows the settings that changed between two definitions.
func (d ItemDiff) SetDefinition(scope string, definition snapshot.DefinitionDiff) ItemDiff {
	d.frame.title = "Definition · " + scope
	d.offset = 0
	d.lines = []string{theme.TextStyle().Render(definition.Summary())}
	for _, change := range definition.Changes {
		d.lines = append(d.lines, theme.TextStyle().Render("  "+change))
	}
	d.footer = "the backend's own names, shown as it gives them"
	return d
}

func (d ItemDiff) ScrollUp() ItemDiff { return d.scroll(-1) }

func (d ItemDiff) ScrollDown() ItemDiff { return d.scroll(1) }

func (d ItemDiff) scroll(delta int) ItemDiff {
	_, height := d.frame.inner()
	d.offset = clampScroll(d.offset+delta, len(d.lines), max(height-itemChrome, 0))
	return d
}

func (d ItemDiff) View() string {
	width, height := d.frame.inner()
	bodyHeight := max(height-itemChrome, 0)
	end := min(d.offset+bodyHeight, len(d.lines))
	body := padBody(d.lines[d.offset:end], bodyHeight)
	body = append(body, theme.HintStyle().Render(fit(d.footer, width)))
	return d.frame.render(strings.Join(append(body, themedHelp(d.hints).ShortHelpView(d.keys)), "\n"))
}

// render marks each line of a line diff, folding the long runs of lines
// that did not change.
func (d ItemDiff) render(lines []snapshot.Line) []string {
	var rendered []string
	for i := 0; i < len(lines); i++ {
		if lines[i].Kind == snapshot.LineSame {
			run := sameRun(lines, i)
			rendered = append(rendered, foldSame(lines[i:i+run], i == 0, i+run == len(lines))...)
			i += run - 1
			continue
		}
		rendered = append(rendered, d.changedLine(lines[i]))
	}
	return rendered
}

func sameRun(lines []snapshot.Line, from int) int {
	n := 0
	for from+n < len(lines) && lines[from+n].Kind == snapshot.LineSame {
		n++
	}
	return n
}

// foldSame keeps diffContext lines of a run next to each change, and
// counts the rest in one line.
func foldSame(run []snapshot.Line, first, last bool) []string {
	keepBefore, keepAfter := diffContext, diffContext
	if first {
		keepBefore = 0
	}
	if last {
		keepAfter = 0
	}
	if len(run) <= keepBefore+keepAfter+1 || first && last {
		return sameLines(run)
	}
	folded := sameLines(run[:keepBefore])
	folded = append(folded, theme.HintStyle().Render(fmt.Sprintf("    … %d unchanged lines", len(run)-keepBefore-keepAfter)))
	return append(folded, sameLines(run[len(run)-keepAfter:])...)
}

func sameLines(run []snapshot.Line) []string {
	lines := make([]string, 0, len(run))
	for _, line := range run {
		lines = append(lines, theme.TextStyle().Render("    "+line.Text))
	}
	return lines
}

func (d ItemDiff) changedLine(line snapshot.Line) string {
	if line.Kind == snapshot.LineRemoved {
		return theme.ErrorStyle().Render("  " + d.icons.Removed + " " + line.Text)
	}
	return theme.SuccessStyle().Render("  + " + line.Text)
}

func structuralLines(before, after []byte) []string {
	changes, err := snapshot.Structural(before, after)
	if err != nil {
		return []string{theme.ErrorStyle().Render(err.Error())}
	}
	lines := []string{theme.HintStyle().Render("too long for a line diff; the fields that changed:")}
	for _, change := range changes {
		lines = append(lines, theme.TextStyle().Render("  "+change.Path+": "+change.Summary()))
	}
	return lines
}

func itemFooter(item snapshot.ItemChange, before, after []byte) string {
	footer := ""
	if item.Kind == snapshot.Modified {
		changes, err := snapshot.Structural(before, after)
		if err == nil {
			footer = changedFieldsText(len(snapshot.FieldNames(changes))) + " · "
		}
	}
	if entry := item.After; entry != nil && entry.ModifiedUnix != 0 {
		return footer + "modified " + entry.Modified().Format("2006-01-02 15:04:05 UTC")
	}
	if entry := item.Before; entry != nil && entry.ModifiedUnix != 0 {
		return footer + "last modified " + entry.Modified().Format("2006-01-02 15:04:05 UTC")
	}
	return strings.TrimSuffix(footer, " · ")
}

func changedFieldsText(n int) string {
	if n == 1 {
		return "1 field"
	}
	return fmt.Sprintf("%d fields", n)
}
