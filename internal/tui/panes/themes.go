package panes

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	themesTitle = "Themes"
	// sideBySideWidth is the narrowest picker that fits the preview beside
	// the list rather than under it.
	sideBySideWidth = 98
	// markWidth is the widest mark a row carries after its name.
	markWidth      = len("this session")
	savedMark      = "saved"
	usedMark       = "this session"
	cannotLoadMark = "cannot load"
)

// ThemeFinder loads a theme by name, for the preview of the one highlighted.
type ThemeFinder func(name string) (theme.Theme, error)

// ThemePicker lists every theme beside a preview of the highlighted one,
// drawn in that theme's own styles whatever theme is active. Its value
// receiver hides a shared cache: keep every ThemePicker it hands back.
type ThemePicker struct {
	frame   frame
	icons   theme.IconSet
	hints   help.Model
	keys    []key.Binding
	entries []theme.Entry
	cursor  int
	saved   string
	inUse   string
	find    ThemeFinder
	// previews holds the styles of every theme highlighted since the picker
	// opened, so moving through the list reads each file once.
	previews map[string]*theme.Styles
}

// NewThemePicker builds the picker; keys are the bindings its hint line
// shows.
func NewThemePicker(icons theme.IconSet, keys []key.Binding) ThemePicker {
	return ThemePicker{frame: frame{title: themesTitle, focused: true}, icons: icons, hints: help.New(), keys: keys}
}

func (p ThemePicker) SetSize(width, height int) ThemePicker {
	p.frame = p.frame.size(width, height)
	return p
}

// Open lists entries, leaving out files that are not themes at all, with
// the cursor on the theme in use. saved is the theme later launches open in.
func (p ThemePicker) Open(entries []theme.Entry, saved, inUse string, find ThemeFinder) ThemePicker {
	p.entries = slices.DeleteFunc(slices.Clone(entries), func(e theme.Entry) bool { return e.Ignored })
	p.saved, p.inUse, p.find = saved, inUse, find
	p.previews = map[string]*theme.Styles{}
	p.cursor = max(slices.IndexFunc(p.entries, func(e theme.Entry) bool { return e.Name == inUse }), 0)
	return p
}

func (p ThemePicker) CursorUp() ThemePicker {
	p.cursor = max(p.cursor-1, 0)
	return p
}

func (p ThemePicker) CursorDown() ThemePicker {
	p.cursor = min(p.cursor+1, max(len(p.entries)-1, 0))
	return p
}

// Highlighted is the theme under the cursor.
func (p ThemePicker) Highlighted() (theme.Entry, bool) {
	if p.cursor >= len(p.entries) {
		return theme.Entry{}, false
	}
	return p.entries[p.cursor], true
}

// Chosen is the highlighted theme when it loads, ready to use.
func (p ThemePicker) Chosen() (theme.Theme, bool) {
	entry, ok := p.Highlighted()
	if !ok || entry.Err != nil {
		return theme.Theme{}, false
	}
	found, err := p.find(entry.Name)
	return found, err == nil
}

func (p ThemePicker) View() string {
	width, height := p.frame.inner()
	hint := themedHelp(p.hints).ShortHelpView(p.keys)
	reason := p.reasonLine(width)
	bodyHeight := max(height-2, 0)
	var body []string
	if width >= sideBySideWidth {
		body = p.sideBySide(width, bodyHeight)
	} else {
		body = p.stacked(width, bodyHeight)
	}
	return p.frame.render(strings.Join(append(padBody(body, bodyHeight), reason, hint), "\n"))
}

// reasonLine says why the highlighted theme cannot be chosen, when it
// cannot.
func (p ThemePicker) reasonLine(width int) string {
	entry, ok := p.Highlighted()
	if !ok || entry.Err == nil {
		return ""
	}
	return theme.ErrorStyle().Render(clipLine(entry.Name+": "+theme.Problem(entry.Err), width))
}

func (p ThemePicker) sideBySide(width, height int) []string {
	listWidth := p.listWidth()
	styles := p.previewStyles()
	list := p.listBox(styles, listWidth, height)
	preview := p.previewBox(styles, width-listWidth, height)
	return strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, list, preview), "\n")
}

// stacked puts the preview under the list, which keeps at least a few rows
// and scrolls through the rest.
func (p ThemePicker) stacked(width, height int) []string {
	styles := p.previewStyles()
	listHeight := min(len(p.entries), max(height-borderCells-previewMinRows-borderCells, minListRows)) + borderCells
	list := p.listBox(styles, width, listHeight)
	preview := p.previewBox(styles, width, height-listHeight)
	return strings.Split(lipgloss.JoinVertical(lipgloss.Left, list, preview), "\n")
}

func (p ThemePicker) listWidth() int {
	names := 0
	for _, entry := range p.entries {
		names = max(names, lipgloss.Width(entry.Name))
	}
	return glyphWidth + names + 2 + markWidth + borderCells
}

// listBox draws the names in the preview's styles, in a blurred frame, so
// the muted role shows as it does on the workspace.
func (p ThemePicker) listBox(styles *theme.Styles, width, height int) string {
	box := frame{width: width, height: height}
	innerWidth, innerHeight := box.inner()
	nameWidth := max(innerWidth-glyphWidth-2-markWidth, 1)
	lines := make([]string, 0, len(p.entries))
	for i, entry := range p.entries {
		lines = append(lines, p.row(styles, entry, i == p.cursor, nameWidth))
	}
	return box.renderIn(styles, strings.Join(window(lines, p.cursor, innerHeight), "\n"))
}

func (p ThemePicker) row(styles *theme.Styles, entry theme.Entry, highlighted bool, nameWidth int) string {
	glyph := strings.Repeat(" ", glyphWidth)
	text := styles.TextStyle()
	if highlighted {
		glyph = styles.AccentStyle().Render(p.icons.Right) + " "
		text = styles.SelectedStyle()
	}
	if entry.Err != nil {
		text = styles.HintStyle()
	}
	return glyph + text.Render(fit(entry.Name, nameWidth)) + "  " + styles.HintStyle().Render(p.mark(entry))
}

func (p ThemePicker) mark(entry theme.Entry) string {
	switch {
	case entry.Err != nil:
		return cannotLoadMark
	case entry.Name == p.saved:
		return savedMark
	case entry.Name == p.inUse:
		return usedMark
	}
	return ""
}

// previewStyles are the highlighted theme's, or the active ones while it
// does not load.
func (p ThemePicker) previewStyles() *theme.Styles {
	entry, ok := p.Highlighted()
	if !ok || entry.Err != nil || p.find == nil {
		return theme.Active()
	}
	if styles, cached := p.previews[entry.Name]; cached {
		return styles
	}
	found, err := p.find(entry.Name)
	if err != nil {
		return theme.Active()
	}
	styles := theme.NewStyles(found)
	p.previews[entry.Name] = styles
	return styles
}
