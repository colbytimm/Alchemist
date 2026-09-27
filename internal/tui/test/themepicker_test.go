package tui_test

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// fakeThemes lists the built-in themes, then its own, and records every
// save.
type fakeThemes struct {
	custom  []theme.Theme
	broken  map[string]error
	saved   string
	saves   []string
	failAll error
}

func (f *fakeThemes) List() []theme.Entry {
	entries := theme.List(theme.Custom{})
	for _, t := range f.custom {
		entries = append(entries, theme.Entry{Name: t.Name(), About: t.About()})
	}
	for name, err := range f.broken {
		entries = append(entries, theme.Entry{Name: name, Err: &theme.LoadError{Theme: name, Err: err}})
	}
	return entries
}

func (f *fakeThemes) Find(name string) (theme.Theme, error) {
	for _, t := range f.custom {
		if t.Name() == name {
			return t, nil
		}
	}
	return theme.Find(name, theme.Custom{})
}

func (f *fakeThemes) Save(name string) error {
	f.saves = append(f.saves, name)
	if f.failAll != nil {
		return f.failAll
	}
	f.saved = name
	return nil
}

func (f *fakeThemes) Saved() string { return f.saved }

// newThemesModel is a loaded session whose picker lists themes, sized to
// width by height.
func newThemesModel(t *testing.T, themes tui.ThemeCatalog, width, height int) tea.Model {
	t.Helper()
	m := newModelWith(t, newOrdersConnection(t), tui.Options{Manage: managed, Themes: themes})
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model, _ := settle(m, m.Init())
	return model
}

func ctrlT() tea.KeyMsg { return keyMsg(tea.KeyCtrlT) }

// highlightTheme moves the picker's cursor down to name.
func highlightTheme(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	for range 20 {
		if strings.Contains(plain(m.View()), fmt.Sprintf("%s %s", theme.Icons().Right, name)) {
			return m
		}
		m = pressAll(t, m, keyMsg(tea.KeyDown))
	}
	require.Fail(t, "never highlighted", name)
	return m
}

func TestThePickerOpensFromTheEditorAndLeavesTheQueryAlone(t *testing.T) {
	m := pressAll(t, typeQuery(t, newThemesModel(t, &fakeThemes{}, testWidth, testHeight), "SELECT ab"), keyMsg(tea.KeyLeft))
	before := m.View()

	opened := pressAll(t, m, ctrlT())
	require.Contains(t, plain(opened.View()), "Themes")
	closed := pressAll(t, opened, keyMsg(tea.KeyEscape))

	assert.Equal(t, before, closed.View(), "the query, its cursor and the editor's focus are as they were")
	typed := pressAll(t, closed, keyRune('x'))
	assert.Contains(t, plain(typed.View()), "SELECT axb", "the cursor stayed between a and b")
}

func TestCtrlTClosesThePickerToo(t *testing.T) {
	m := newThemesModel(t, &fakeThemes{}, testWidth, testHeight)

	closed := pressAll(t, m, ctrlT(), ctrlT())

	assert.Equal(t, m.View(), closed.View())
}

func TestBrowsingChangesNothingOutsideThePicker(t *testing.T) {
	a := distinctTheme(t, "a", 0x10)
	useTheme(t, a)
	themes := &fakeThemes{custom: []theme.Theme{a, distinctTheme(t, "b", 0x20)}}
	m := newThemesModel(t, themes, testWidth, testHeight)
	before, styles := m.View(), theme.Active()

	closed := pressAll(t, m, ctrlT(), keyMsg(tea.KeyDown), keyMsg(tea.KeyDown), keyMsg(tea.KeyEscape))

	assert.Equal(t, before, closed.View())
	assert.Same(t, styles, theme.Active())
	assert.Empty(t, themes.saves)
}

func TestThePreviewShowsTheHighlightedTheme(t *testing.T) {
	a, b := distinctTheme(t, "a", 0x10), distinctTheme(t, "b", 0x20)
	useTheme(t, a)
	m := newThemesModel(t, &fakeThemes{custom: []theme.Theme{a, b}}, 120, 40)

	view := highlightTheme(t, pressAll(t, m, ctrlT()), "b").View()

	for _, role := range theme.Roles() {
		assert.Contains(t, view, foreground(b, role), "the preview shows %s", role)
	}
	assert.Contains(t, view, underline(b), "the squiggle is the theme's error color")
	sample := plain(view)
	for _, want := range []string{"CONTAIN(", "unknown function CONTAIN", "id     status", "o003", "! warning", "1.00 RU"} {
		assert.Contains(t, sample, want)
	}
	assert.Equal(t, "a", theme.Active().Theme().Name(), "browsing leaves the active theme alone")
}

// underline is the escape code that colors a squiggle in t's error color.
func underline(t theme.Theme) string {
	rgb, _ := strconv.ParseUint(strings.TrimPrefix(t.Color(theme.Error).Dark, "#"), 16, 32)
	return fmt.Sprintf("58:2::%d:%d:%d", rgb>>16, rgb>>8&0xff, rgb&0xff)
}

func TestEnterAppliesAndSavesOnce(t *testing.T) {
	a, b := distinctTheme(t, "a", 0x10), distinctTheme(t, "b", 0x20)
	useTheme(t, a)
	themes := &fakeThemes{custom: []theme.Theme{a, b}}
	m := highlightTheme(t, pressAll(t, newThemesModel(t, themes, testWidth, testHeight), ctrlT()), "b")

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Equal(t, []string{"b"}, themes.saves)
	assert.Equal(t, "b", theme.Active().Theme().Name())
	assert.Contains(t, plain(m.View()), "theme b saved")
	assert.Contains(t, plain(m.View()), catalogTitle, "the picker is closed")
}

func TestSwitchingThroughThePickerLeavesNoOldColor(t *testing.T) {
	a, b := distinctTheme(t, "a", 0x10), distinctTheme(t, "b", 0x20)
	useTheme(t, a)
	themes := &fakeThemes{custom: []theme.Theme{a, b}}
	m := runQuery(t, selectContainer(t, newThemesModel(t, themes, testWidth, testHeight)), highlightedQuery)

	m = pressAll(t, highlightTheme(t, pressAll(t, m, ctrlT()), "b"), keyMsg(tea.KeyEnter))

	view := m.View()
	for _, role := range theme.Roles() {
		assert.NotContains(t, view, foreground(a, role), "%s is still drawn in the old theme", role)
	}
	assert.Contains(t, view, foreground(b, theme.Keyword))
	assert.Contains(t, plain(view), "UPPER(c.status)", "the query is still there")
}

func TestAFailedSaveStillAppliesAndSaysWhy(t *testing.T) {
	a, b := distinctTheme(t, "a", 0x10), distinctTheme(t, "b", 0x20)
	useTheme(t, a)
	themes := &fakeThemes{custom: []theme.Theme{a, b}, failAll: errors.New("config.toml: permission denied")}
	m := highlightTheme(t, pressAll(t, newThemesModel(t, themes, 120, testHeight), ctrlT()), "b")

	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Equal(t, "b", theme.Active().Theme().Name())
	assert.Contains(t, plain(m.View()), "theme b in use for this session; not saved: config.toml: permission denied")
}

func TestABrokenThemeIsListedButCannotBeChosen(t *testing.T) {
	useTheme(t, theme.Default())
	themes := &fakeThemes{broken: map[string]error{"broken": errors.New(`keyword: "#12345" is not #RRGGBB`)}}
	m := highlightTheme(t, pressAll(t, newThemesModel(t, themes, testWidth, testHeight), ctrlT()), "broken")
	styles := theme.Active()

	view := plain(m.View())
	m = pressAll(t, m, keyMsg(tea.KeyEnter))

	assert.Contains(t, view, "cannot load")
	assert.Contains(t, view, `broken: keyword: "#12345" is not #RRGGBB`)
	assert.Same(t, styles, theme.Active())
	assert.Empty(t, themes.saves)
	assert.Contains(t, plain(m.View()), "Themes", "the picker stays open")
}

func TestWithoutACatalogThePickerAppliesWithoutSaving(t *testing.T) {
	useTheme(t, theme.Default())
	m := pressAll(t, newThemesModel(t, nil, testWidth, testHeight), ctrlT())
	for _, name := range theme.BuiltinNames() {
		assert.Contains(t, plain(m.View()), name)
	}

	m = pressAll(t, highlightTheme(t, m, "jarvis-hud"), keyMsg(tea.KeyEnter))

	assert.Equal(t, "jarvis-hud", theme.Active().Theme().Name())
	assert.Contains(t, plain(m.View()), "theme jarvis-hud in use for this session; not saved")
}

func TestThePickerMarksTheSavedThemeAndTheOneInUse(t *testing.T) {
	useTheme(t, theme.Default())
	m := newThemesModel(t, &fakeThemes{saved: "jarvis-hud"}, 120, testHeight)

	view := plain(pressAll(t, m, ctrlT()).View())

	assert.Regexp(t, `jarvis-hud\s+saved`, view)
	assert.Regexp(t, `alchemist\s+this session`, view)
}

func TestThePickerFitsSmallTerminalsAndScrolls(t *testing.T) {
	var many []theme.Theme
	for i := range 12 {
		many = append(many, distinctTheme(t, fmt.Sprintf("custom-%02d", i), 0x30+i))
	}
	for _, size := range []struct{ width, height int }{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			useTheme(t, theme.Default())
			m := pressAll(t, newThemesModel(t, &fakeThemes{custom: many}, size.width, size.height), ctrlT())

			m = highlightTheme(t, m, "custom-11")

			view := m.View()
			assert.LessOrEqual(t, lipgloss.Height(view), size.height)
			assert.LessOrEqual(t, lipgloss.Width(view), size.width)
			for _, want := range []string{"custom-11", "SELECT o.id", "id     status", "o003", "1.00 RU", "choose"} {
				assert.Contains(t, plain(view), want)
			}
		})
	}
}

func TestThePreviewSaysWhenColorsAreOff(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })
	themes := &fakeThemes{}

	m := pressAll(t, newThemesModel(t, themes, 120, 40), ctrlT())
	assert.Contains(t, m.View(), "colors are off (NO_COLOR)")

	pressAll(t, highlightTheme(t, m, "jarvis-hud"), keyMsg(tea.KeyEnter))
	assert.Equal(t, []string{"jarvis-hud"}, themes.saves, "the picker still saves")
	theme.Use(theme.Default())
}

func TestTheStartNoticeShowsInTheStatusBar(t *testing.T) {
	m := newModelWith(t, newConnection(t), tui.Options{Notice: "theme mine not loaded: unknown theme; using alchemist"})

	m, _ = settle(m, m.Init())

	assert.Contains(t, plain(m.View()), "theme mine not loaded")
}
