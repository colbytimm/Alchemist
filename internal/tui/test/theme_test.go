package tui_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// distinctTheme gives every role a color no other role, and no theme built
// from another seed, uses, so which theme drew a screen can be read off it.
func distinctTheme(t *testing.T, name string, seed int) theme.Theme {
	t.Helper()
	var file strings.Builder
	file.WriteString("[about]\nbackground = \"#101010\"\n[colors]\n")
	for _, role := range theme.Roles() {
		fmt.Fprintf(&file, "%s = \"#%02X%02X%02X\"\n", role, seed, int(role)+1, 0x40+seed)
	}
	parsed, err := theme.Parse(name, []byte(file.String()))
	require.NoError(t, err)
	return parsed
}

// foreground is the escape code that draws text in role's color of t.
func foreground(t theme.Theme, role theme.Role) string {
	hex := t.Color(role).Dark
	rgb, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return fmt.Sprintf("38;2;%d;%d;%dm", rgb>>16, rgb>>8&0xff, rgb&0xff)
}

// useTheme makes t the active theme for the length of the test.
func useTheme(tb testing.TB, t theme.Theme) {
	tb.Helper()
	theme.Use(t)
	tb.Cleanup(func() { theme.Use(theme.Default()) })
}

// themedScreens are the screens every role shows on, each built by a key
// press from a session: the workspace with a highlighted query, results
// and a charge, with the editor focused and blurred, and the overlays whose
// panes draw through components that keep styles of their own.
func themedScreens(t *testing.T) map[string]tea.Model {
	t.Helper()
	store := &recordingStore{}
	editing := runQuery(t, selectContainer(t, newHistoryModel(t, newOrdersConnection(t), store)), highlightedQuery)
	browsing := pressAll(t, editing, keyMsg(tea.KeyTab))
	return map[string]tea.Model{
		"editor focused": editing,
		"editor blurred": browsing,
		"help":           pressAll(t, browsing, keyRune('?')),
		"history":        openHistory(t, browsing),
		"accounts":       pressAll(t, browsing, keyMsg(tea.KeyCtrlG)),
		"export":         pressAll(t, browsing, keyMsg(tea.KeyCtrlE)),
		"save prompt":    pressAll(t, browsing, keyMsg(tea.KeyCtrlS)),
		"batch review":   openReview(t, newBatchModel(t, newOrdersConnection(t), &recordingStore{}), writingBatch),
		"empty editor":   pressAll(t, newLoadedModel(t, newConnection(t)), keyRune('e')),
		"connect form":   newSession(t, newConnection(t), tui.Options{Diagnostics: theme.NoUnderline}),
	}
}

func TestEveryPaneDrawsInTheActiveTheme(t *testing.T) {
	a := distinctTheme(t, "a", 0x10)
	useTheme(t, a)

	view := themedScreens(t)["editor blurred"].View()

	for _, role := range []theme.Role{theme.Text, theme.Muted, theme.Accent, theme.Selected, theme.Success,
		theme.Keyword, theme.Operator, theme.Literal, theme.Function, theme.Alias, theme.Parameter,
		theme.String, theme.Number, theme.Comment, theme.Punctuation} {
		assert.Contains(t, view, foreground(a, role), role.String())
	}
}

func TestASwitchLeavesNoColorOfTheOldTheme(t *testing.T) {
	a, b := distinctTheme(t, "a", 0x10), distinctTheme(t, "b", 0x20)
	useTheme(t, a)
	screens := themedScreens(t)

	theme.Use(b)

	for name, screen := range screens {
		t.Run(name, func(t *testing.T) {
			view := screen.View()
			for _, role := range theme.Roles() {
				assert.NotContains(t, view, foreground(a, role), "%s is still drawn in the old theme", role)
			}
			assert.Contains(t, view, foreground(b, theme.Text))
		})
	}
}
