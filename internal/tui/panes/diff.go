package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	diffTitle       = "Diff"
	diffFilterHint  = "filter by partition key or id"
	diffTimeLayout  = "01-02 15:04"
	keyColumnWidth  = 12
	idColumnWidth   = 16
	definitionLabel = "definition"
	// diffChrome is the summary line and the blank under it, the filter line,
	// the blank over the footer, the footer and the hint.
	diffChrome = 6
)

// changeFilter is one of what tab cycles through: every change, or one
// kind of them.
type changeFilter struct {
	name string
	all  bool
	kind snapshot.ChangeKind
}

func (f changeFilter) keeps(kind snapshot.ChangeKind) bool { return f.all || f.kind == kind }

var changeFilters = []changeFilter{
	{name: "all", all: true},
	{name: "added", kind: snapshot.Added},
	{name: "removed", kind: snapshot.Removed},
	{name: "modified", kind: snapshot.Modified},
}

// diffRow is one row of the list: the definition's, or an item's.
type diffRow struct {
	definition bool
	item       snapshot.ItemChange
}

// Diff is the overlay listing what changed between two snapshots of one
// container. Its fields column is filled in by SetFields for the rows on
// screen, which Unread names, since it takes both bodies. Like the filter
// input it wraps, its value receiver hides shared pointers, so a caller
// must keep every Diff it is handed.
type Diff struct {
	frame      frame
	icons      theme.IconSet
	hints      help.Model
	keys       []key.Binding
	diff       snapshot.Diff
	filter     int
	list       filterList[diffRow]
	fields     map[snapshot.Key]string
	definition bool
}

func NewDiff(icons theme.IconSet, keys []key.Binding) Diff {
	hints := help.New()
	hints.Styles = helpStyles()
	return Diff{
		frame: frame{title: diffTitle, focused: true},
		icons: icons,
		hints: hints,
		keys:  keys,
		list:  newFilterList(diffFilterHint, matchesChange),
	}
}

func (d Diff) SetSize(width, height int) Diff {
	d.frame = d.frame.size(width, height)
	width, _ = d.frame.inner()
	d.list = d.list.setWidth(width)
	d.hints.Width = width
	return d
}

// SetDiff shows diff of the container scope names, from its first row.
func (d Diff) SetDiff(scope string, diff snapshot.Diff) Diff {
	d.frame.title = fmt.Sprintf("%s · %s · %s → %s", diffTitle, scope, diff.From.Time().Format(diffTimeLayout), diff.To.Time().Format(diffTimeLayout))
	d.diff = diff
	d.fields = map[snapshot.Key]string{}
	d.definition = len(diff.Definition.Changes) > 0 || diff.Definition.State != snapshot.DefinitionCompared
	d.filter = 0
	d.list = d.list.clearFilter()
	return d.refilter()
}

func (d Diff) refilter() Diff {
	filter := changeFilters[d.filter]
	var rows []diffRow
	if d.definition && filter.all {
		rows = append(rows, diffRow{definition: true})
	}
	for _, item := range d.diff.Items {
		if filter.keeps(item.Kind) {
			rows = append(rows, diffRow{item: item})
		}
	}
	d.list = d.list.setItems(rows).moveCursor(0)
	return d
}

func matchesChange(row diffRow, needle string) bool {
	if row.definition {
		return strings.Contains(definitionLabel, needle)
	}
	return strings.Contains(strings.ToLower(row.item.Identity.PartitionKeyText()), needle) ||
		strings.Contains(strings.ToLower(row.item.Identity.ID), needle)
}

// CycleKind shows all changes, then the added, removed and modified alone.
func (d Diff) CycleKind() Diff {
	d.filter = (d.filter + 1) % len(changeFilters)
	return d.refilter()
}

func (d Diff) StartFilter() Diff {
	d.list = d.list.startFilter()
	return d
}

func (d Diff) Filtering() bool { return d.list.filtering() }

func (d Diff) ClearFilter() Diff {
	d.list = d.list.clearFilter()
	return d
}

func (d Diff) Update(msg tea.KeyMsg) (Diff, tea.Cmd) {
	var cmd tea.Cmd
	d.list, cmd = d.list.update(msg)
	return d, cmd
}

func (d Diff) CursorUp() Diff {
	d.list = d.list.moveCursor(-1)
	return d
}

func (d Diff) CursorDown() Diff {
	d.list = d.list.moveCursor(1)
	return d
}

func (d Diff) Diff() snapshot.Diff { return d.diff }

// Selected is the item under the cursor; definition is true on the
// definition's row.
func (d Diff) Selected() (item snapshot.ItemChange, definition bool, ok bool) {
	row, ok := d.list.selected()
	return row.item, row.definition, ok
}

// Unread lists the modified items on screen whose fields are not known yet.
func (d Diff) Unread() []snapshot.ItemChange {
	width, height := d.frame.inner()
	matching := d.list.matching()
	start, end := windowBounds(len(matching), d.list.cursor, d.bodyHeight(height, len(packHints(d.hints, d.keys, width))))
	var unread []snapshot.ItemChange
	for _, row := range matching[start:end] {
		if _, known := d.fields[row.item.Key]; !row.definition && row.item.Kind == snapshot.Modified && !known {
			unread = append(unread, row.item)
		}
	}
	return unread
}

// SetFields files the changed fields of items by key.
func (d Diff) SetFields(fields map[snapshot.Key]string) Diff {
	for k, text := range fields {
		d.fields[k] = text
	}
	return d
}

func (d Diff) View() string {
	width, height := d.frame.inner()
	hints := packHints(d.hints, d.keys, width)
	bodyHeight := d.bodyHeight(height, len(hints))
	lines := []string{theme.TextStyle().Render(fit(d.summary(), width)), "", d.list.filterLine()}
	lines = append(lines, padBody(d.rows(width, bodyHeight), bodyHeight)...)
	lines = append(lines, "", theme.HintStyle().Render(fit(d.footer(), width)))
	return d.frame.renderWithHints(lines, hints)
}

func (d Diff) bodyHeight(height, hints int) int {
	return max(height-diffChrome-hints+1, 0)
}

func (d Diff) summary() string {
	return fmt.Sprintf("+%s added   %s%s removed   ~%s modified   %s", FormatCount(int64(d.diff.Count(snapshot.Added))),
		d.icons.Removed, FormatCount(int64(d.diff.Count(snapshot.Removed))), FormatCount(int64(d.diff.Count(snapshot.Modified))),
		d.diff.Definition.Summary())
}

func (d Diff) footer() string {
	items := 0
	for _, row := range d.list.matching() {
		if !row.definition {
			items++
		}
	}
	footer := fmt.Sprintf("%s · %s changes", changeFilters[d.filter].name, FormatCount(int64(items)))
	if moved := d.diff.MovedIDs(); len(moved) > 0 {
		footer += fmt.Sprintf(" · %s changed partition key: a removal and an addition", strings.Join(moved, ", "))
	}
	return footer
}

func (d Diff) rows(width, height int) []string {
	matching := d.list.matching()
	if len(matching) == 0 {
		return []string{theme.HintStyle().Render("no changes")}
	}
	start, end := windowBounds(len(matching), d.list.cursor, height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, d.row(matching[i], width, i == d.list.cursor))
	}
	return lines
}

func (d Diff) row(row diffRow, width int, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "› "
	}
	if row.definition {
		text := d.diff.Definition.Summary()
		if len(d.diff.Definition.Changes) > 0 {
			text = d.diff.Definition.Changes[0]
		}
		return d.style(snapshot.Modified, selected).Render(fit(cursor+"  "+definitionLabel+"   "+text, width))
	}
	item := row.item
	text := fmt.Sprintf("%s%s %s %s %s", cursor, d.sign(item.Kind), fit(item.Identity.PartitionKeyText(), keyColumnWidth),
		fit(item.Identity.ID, idColumnWidth), d.fields[item.Key])
	return d.style(item.Kind, selected).Render(fit(text, width))
}

func (d Diff) sign(kind snapshot.ChangeKind) string {
	switch kind {
	case snapshot.Added:
		return "+"
	case snapshot.Removed:
		return d.icons.Removed
	}
	return "~"
}

func (d Diff) style(kind snapshot.ChangeKind, selected bool) lipgloss.Style {
	style := theme.TextStyle()
	switch kind {
	case snapshot.Added:
		style = theme.SuccessStyle()
	case snapshot.Removed:
		style = theme.ErrorStyle()
	}
	return style.Bold(selected)
}
