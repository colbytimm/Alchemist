package theme_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

// adaptedPalettes are the colors mapped from each source theme, in role
// order, exactly as the source files write them.
var adaptedPalettes = map[string][]string{
	"jarvis-hud": {
		"#c5ccd6", "#7d8794", "#00d9ff", "#00e5ff", "#5ce6a6", "#ffb454", "#ff4d5e", "#00e5ff", "#9fa9b7",
		"#ff6ec7", "#ffc94d", "#dfe4ea", "#8ef2c4", "#cfe88a", "#ff8c42", "#59636e", "#6b7480",
	},
	"dracula-at-midnight": {
		"#F8F8F2", "#9d9d9d", "#FF79C6", "#8BE9FD", "#50FA7B", "#8BE9FD", "#FF5555", "#FF79C6", "#FF79C6",
		"#FFB86C", "#8BE9FD", "#F8F8F2", "#FFB86C", "#F1FA8C", "#FFB86C", "#9d9d9d", "#F8F8F2",
	},
	"enchanted-grove-dark": {
		"#FFFFFF", "#a3c9bb", "#7A9A42", "#7FC76A", "#56B56A", "#ebcb8b", "#BF616A", "#E88787", "#E88787",
		"#B8A3FF", "#A3C5F0", "#FFFFFF", "#FFB366", "#7AB87A", "#FFB366", "#c4d0ba", "#E8F5E8",
	},
}

func TestAdaptedThemesMatchTheMappingTable(t *testing.T) {
	for name, palette := range adaptedPalettes {
		found, err := theme.Find(name, theme.Custom{})
		require.NoError(t, err)
		for _, role := range theme.Roles() {
			t.Run(name+"/"+role.String(), func(t *testing.T) {
				got := found.Color(role)
				assert.Equal(t, palette[role], got.Dark)
				assert.Equal(t, got.Dark, got.Light, "an adapted theme is dark only")
			})
		}
	}
}

func TestEveryAdaptedThemeIsCredited(t *testing.T) {
	for name := range adaptedPalettes {
		about := mustFindTheme(t, name).About()

		assert.NotEmpty(t, about.Author, name)
		assert.Equal(t, "MIT", about.License, name)
		assert.NotEmpty(t, about.Source, name)
		assert.Regexp(t, `^#[0-9A-Fa-f]{6}$`, about.Background, name)
	}
}

func TestOnlyLicensedThemesShip(t *testing.T) {
	assert.ElementsMatch(t, []string{"alchemist", "jarvis-hud", "dracula-at-midnight", "enchanted-grove-dark"}, theme.BuiltinNames())
}

func mustFindTheme(t *testing.T, name string) theme.Theme {
	t.Helper()
	found, err := theme.Find(name, theme.Custom{})
	require.NoError(t, err)
	return found
}
