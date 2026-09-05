package theme_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

func TestLogoContainsAllLines(t *testing.T) {
	logo := theme.Logo()
	require.NotEmpty(t, logo)
	assert.Equal(t, len(strings.Split(theme.RawLogo, "\n")), len(strings.Split(logo, "\n")))
}

func TestPaletteColorsAreAdaptivePairs(t *testing.T) {
	colors := map[string]struct{ light, dark string }{
		"Gold":      {theme.Gold().Light, theme.Gold().Dark},
		"Copper":    {theme.Copper().Light, theme.Copper().Dark},
		"Verdigris": {theme.Verdigris().Light, theme.Verdigris().Dark},
		"Amethyst":  {theme.Amethyst().Light, theme.Amethyst().Dark},
		"Parchment": {theme.Parchment().Light, theme.Parchment().Dark},
		"Cinnabar":  {theme.Cinnabar().Light, theme.Cinnabar().Dark},
		"Ash":       {theme.Ash().Light, theme.Ash().Dark},
	}
	for name, c := range colors {
		assert.Regexp(t, `^#[0-9A-Fa-f]{6}$`, c.light, "%s light", name)
		assert.Regexp(t, `^#[0-9A-Fa-f]{6}$`, c.dark, "%s dark", name)
		assert.NotEqual(t, c.light, c.dark, "%s should differ between light and dark", name)
	}
}

func TestStylesRenderContent(t *testing.T) {
	styles := map[string]func(...string) string{
		"LogoStyle":          theme.LogoStyle().Render,
		"TaglineStyle":       theme.TaglineStyle().Render,
		"TextStyle":          theme.TextStyle().Render,
		"HintStyle":          theme.HintStyle().Render,
		"ErrorStyle":         theme.ErrorStyle().Render,
		"SuccessStyle":       theme.SuccessStyle().Render,
		"FocusedBorderStyle": theme.FocusedBorderStyle().Render,
		"BlurredBorderStyle": theme.BlurredBorderStyle().Render,
	}
	for name, render := range styles {
		assert.Contains(t, render("sample"), "sample", name)
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	icons := theme.Icons()
	icons.Database = "mutated"
	assert.NotEqual(t, "mutated", theme.Icons().Database, "Icons must not hand out shared state")

	gold := theme.Gold()
	gold.Light = "#000000"
	assert.NotEqual(t, "#000000", theme.Gold().Light, "Gold must not hand out shared state")
}

func TestIconSetsAreComplete(t *testing.T) {
	for _, icons := range []theme.IconSet{theme.Icons(), theme.ASCIIIcons()} {
		assert.NotEmpty(t, icons.Database)
		assert.NotEmpty(t, icons.Container)
		assert.NotEmpty(t, icons.Expanded)
		assert.NotEmpty(t, icons.Collapsed)
		assert.NotEmpty(t, icons.Success)
		assert.NotEmpty(t, icons.Failure)
		assert.NotEmpty(t, icons.Spinner)
	}
}
