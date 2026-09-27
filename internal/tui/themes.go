package tui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/theme"
)

var errNoThemeStore = errors.New("this session has nowhere to save it")

// themeSavedMsg reports saving the theme just put in use; Err is nil when
// it was saved.
type themeSavedMsg struct {
	Theme string
	Err   error
}

// openThemes lists the themes afresh, so a file written during the session
// is there, and leaves the panes behind the picker exactly as they were.
func (m Model) openThemes() Model {
	m.themePicker = m.themePicker.Open(m.themeEntries(), m.savedTheme(), theme.Active().Theme().Name(), m.findTheme)
	m.overlay = overlayThemes
	return m
}

func (m Model) themeEntries() []theme.Entry {
	if m.themes == nil {
		return theme.List(theme.Custom{})
	}
	return m.themes.List()
}

// savedTheme is the theme later launches open in: with none saved, the
// default. A session that cannot save has none.
func (m Model) savedTheme() string {
	if m.themes == nil {
		return ""
	}
	if saved := m.themes.Saved(); saved != "" {
		return saved
	}
	return theme.DefaultName
}

func (m Model) findTheme(name string) (theme.Theme, error) {
	if m.themes == nil {
		return theme.Find(name, theme.Custom{})
	}
	return m.themes.Find(name)
}

// handleThemesKey moves through the list, which changes only the preview;
// nothing outside the picker changes until enter.
func (m Model) handleThemesKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close), key.Matches(msg, m.keys.Themes):
		m.overlay = overlayNone
	case key.Matches(msg, m.keys.Up):
		m.themePicker = m.themePicker.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.themePicker = m.themePicker.CursorDown()
	case key.Matches(msg, m.keys.UseTheme):
		return m.useTheme()
	}
	return m, nil
}

// useTheme switches the whole screen to the highlighted theme at once, then
// saves it for every later launch. A theme that does not load cannot be
// chosen.
func (m Model) useTheme() (Model, tea.Cmd) {
	chosen, ok := m.themePicker.Chosen()
	if !ok {
		return m, nil
	}
	theme.Use(chosen)
	m.overlay = overlayNone
	if m.themes == nil {
		return m, func() tea.Msg { return themeSavedMsg{Theme: chosen.Name(), Err: errNoThemeStore} }
	}
	return m, saveTheme(m.themes, chosen.Name())
}

func saveTheme(themes ThemeCatalog, name string) tea.Cmd {
	return func() tea.Msg {
		return themeSavedMsg{Theme: name, Err: themes.Save(name)}
	}
}

func (m Model) finishThemeSave(msg themeSavedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.logger.Error("theme not saved", "theme", msg.Theme, "error", msg.Err)
		return m.notify(fmt.Sprintf("theme %s in use for this session; not saved: %v", msg.Theme, msg.Err))
	}
	return m.notify(fmt.Sprintf("theme %s saved", msg.Theme))
}
