package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/theme"
)

// groupChrome is the blank line and the hint.
const groupChrome = 2

// GroupDiff lists the containers of two database snapshots with what
// changed in each.
type GroupDiff struct {
	frame   frame
	icons   theme.IconSet
	hints   help.Model
	keys    []key.Binding
	changes []snapshot.GroupChange
	cursor  int
}

func NewGroupDiff(icons theme.IconSet, keys []key.Binding) GroupDiff {
	hints := help.New()
	hints.Styles = helpStyles()
	return GroupDiff{frame: frame{title: diffTitle, focused: true}, icons: icons, hints: hints, keys: keys}
}

func (g GroupDiff) SetSize(width, height int) GroupDiff {
	g.frame = g.frame.size(width, height)
	width, _ = g.frame.inner()
	g.hints.Width = width
	return g
}

func (g GroupDiff) SetChanges(scope string, from, to snapshot.Group, changes []snapshot.GroupChange) GroupDiff {
	g.frame.title = fmt.Sprintf("%s · %s · %s → %s", diffTitle, scope, groupTime(from), groupTime(to))
	g.changes, g.cursor = changes, 0
	return g
}

func groupTime(g snapshot.Group) string {
	return snapshot.Record{ID: g.ID, Started: g.Started}.Time().Format(diffTimeLayout)
}

func (g GroupDiff) CursorUp() GroupDiff {
	g.cursor = max(g.cursor-1, 0)
	return g
}

func (g GroupDiff) CursorDown() GroupDiff {
	g.cursor = min(g.cursor+1, max(len(g.changes)-1, 0))
	return g
}

// Selected is the container under the cursor, when both snapshots hold it.
func (g GroupDiff) Selected() (snapshot.GroupChange, bool) {
	if g.cursor >= len(g.changes) || g.changes[g.cursor].State != snapshot.ContainerCompared {
		return snapshot.GroupChange{}, false
	}
	return g.changes[g.cursor], true
}

func (g GroupDiff) View() string {
	width, height := g.frame.inner()
	start, end := windowBounds(len(g.changes), g.cursor, max(height-groupChrome, 0))
	var lines []string
	for i := start; i < end; i++ {
		style, cursor := theme.TextStyle(), "  "
		if i == g.cursor {
			style, cursor = theme.SelectedStyle(), "› "
		}
		lines = append(lines, style.Render(fit(cursor+g.describe(g.changes[i]), width)))
	}
	if len(g.changes) == 0 {
		lines = []string{theme.HintStyle().Render("no containers")}
	}
	return g.frame.renderWithHint(lines, g.hints.ShortHelpView(g.keys))
}

func (g GroupDiff) describe(change snapshot.GroupChange) string {
	name := fit(change.Container, idColumnWidth+keyColumnWidth)
	switch change.State {
	case snapshot.ContainerAppeared:
		return name + " appeared"
	case snapshot.ContainerDisappeared:
		return name + " disappeared"
	case snapshot.ContainerNotCompared:
		return name + " not captured in both"
	}
	return strings.TrimRight(fmt.Sprintf("%s +%s %s%s ~%s", name, FormatCount(int64(change.Added)),
		g.icons.Removed, FormatCount(int64(change.Removed)), FormatCount(int64(change.Modified))), " ")
}
