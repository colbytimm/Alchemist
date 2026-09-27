package theme_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/theme"
)

func TestStylesRenderContent(t *testing.T) {
	styles := map[string]func(...string) string{
		"TextStyle":          theme.TextStyle().Render,
		"HintStyle":          theme.HintStyle().Render,
		"ErrorStyle":         theme.ErrorStyle().Render,
		"SuccessStyle":       theme.SuccessStyle().Render,
		"FocusedBorderStyle": theme.FocusedBorderStyle().Render,
		"BlurredBorderStyle": theme.BlurredBorderStyle().Render,
		"SelectedStyle":      theme.SelectedStyle().Render,
		"SpinnerStyle":       theme.SpinnerStyle().Render,
		"AccentStyle":        theme.AccentStyle().Render,
		"HeadingStyle":       theme.HeadingStyle().Render,
		"WarningStyle":       theme.WarningStyle().Render,
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
