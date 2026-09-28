package theme_test

import (
	"testing"

	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

// use makes t the active theme for the length of the test.
func use(tb testing.TB, t theme.Theme) {
	tb.Helper()
	theme.Use(t)
	tb.Cleanup(func() { theme.Use(theme.Default()) })
}

func TestTheDefaultIsActiveUntilAnotherIsUsed(t *testing.T) {
	assert.Equal(t, theme.DefaultName, theme.Active().Theme().Name())
}

func TestUseSwitchesEveryAccessor(t *testing.T) {
	withTerminal(t, termenv.TrueColor)
	a := distinctTheme(t, "a", 0x10)
	want := theme.NewStyles(a)

	use(t, a)

	assert.Equal(t, want.TextStyle().Render("x"), theme.TextStyle().Render("x"))
	assert.Equal(t, want.SyntaxKeyword().Render("x"), theme.SyntaxKeyword().Render("x"))
	assert.Equal(t, a.Color(theme.Error), theme.DiagnosticError())
}

func TestUseChangesTheActivePointer(t *testing.T) {
	before := theme.Active()

	use(t, distinctTheme(t, "a", 0x10))

	assert.NotSame(t, before, theme.Active())
}

func TestNoColorDrawsEveryThemePlain(t *testing.T) {
	withTerminal(t, termenv.Ascii)
	for _, name := range theme.BuiltinNames() {
		found, err := theme.Find(name, theme.Custom{})
		require.NoError(t, err)
		styles := theme.NewStyles(found)

		for role, style := range roleStyles {
			assert.Equal(t, "x", style(styles).Render("x"), "%s %s", name, role)
		}
	}
}
