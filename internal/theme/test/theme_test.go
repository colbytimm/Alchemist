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
		"SelectedStyle":      theme.SelectedStyle().Render,
		"SpinnerStyle":       theme.SpinnerStyle().Render,
	}
	for name, render := range styles {
		assert.Contains(t, render("sample"), "sample", name)
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	icons := theme.Icons()
	icons.Database = "mutated"
	assert.NotEqual(t, "mutated", theme.Icons().Database, "Icons must not hand out shared state")

	icons.SpinnerFrames[0] = "mutated"
	assert.NotEqual(t, "mutated", theme.Icons().SpinnerFrames[0], "spinner frames must be copied, not aliased")

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
		assert.NotEmpty(t, icons.Left)
		assert.NotEmpty(t, icons.Right)
		assert.NotEmpty(t, icons.Success)
		assert.NotEmpty(t, icons.Failure)
		assert.NotEmpty(t, icons.Separator)
		assert.NotEmpty(t, icons.BarFull)
		assert.NotEmpty(t, icons.BarEmpty)
		assert.GreaterOrEqual(t, len(icons.SpinnerFrames), 2, "an animation needs more than one frame")
	}
}

func TestASCIIIconsAreASCIIOnly(t *testing.T) {
	icons := theme.ASCIIIcons()
	glyphs := append([]string{
		icons.Database, icons.Container, icons.Expanded, icons.Collapsed,
		icons.Left, icons.Right, icons.Success, icons.Failure, icons.Separator,
		icons.BarFull, icons.BarEmpty,
	}, icons.SpinnerFrames...)

	for _, glyph := range glyphs {
		assert.Equal(t, len(glyph), len([]rune(glyph)), "%q is not ASCII", glyph)
	}
}
