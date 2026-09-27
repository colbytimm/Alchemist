package theme_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/theme"
)

const customDir = "/home/ada/.config/alchemist/themes"

// customThemes is a themes folder holding the given files.
func customThemes(files map[string]string) theme.Custom {
	folder := fstest.MapFS{}
	for name, data := range files {
		folder[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return theme.Custom{Dir: customDir, Files: folder}
}

func TestEveryBuiltinThemeParses(t *testing.T) {
	for _, name := range theme.BuiltinNames() {
		_, err := theme.Find(name, theme.Custom{})

		require.NoError(t, err, name)
	}
}

func TestBuiltinFilesAreAllListed(t *testing.T) {
	files, err := fs.Glob(os.DirFS("../themes"), "*.toml")
	require.NoError(t, err)

	var names []string
	for _, file := range files {
		names = append(names, strings.TrimSuffix(file, ".toml"))
	}
	assert.ElementsMatch(t, names, theme.BuiltinNames())
}

func TestAlchemistIsTheDefault(t *testing.T) {
	assert.Equal(t, theme.DefaultName, theme.Default().Name())
	assert.Contains(t, theme.BuiltinNames(), theme.DefaultName)
}

func TestAlchemistKeepsTodaysPalette(t *testing.T) {
	var (
		parchment = lipgloss.AdaptiveColor{Light: "#5C4B37", Dark: "#E8DCC8"}
		ash       = lipgloss.AdaptiveColor{Light: "#8A8378", Dark: "#6B655B"}
		gold      = lipgloss.AdaptiveColor{Light: "#D4A017", Dark: "#F5C542"}
		copper    = lipgloss.AdaptiveColor{Light: "#B87333", Dark: "#D48F52"}
		verdigris = lipgloss.AdaptiveColor{Light: "#2E8B84", Dark: "#5FD3CE"}
		cinnabar  = lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#E74C3C"}
		amethyst  = lipgloss.AdaptiveColor{Light: "#6C3FA0", Dark: "#9B6FD0"}
	)
	want := map[theme.Role]lipgloss.AdaptiveColor{
		theme.Text: parchment, theme.Muted: ash, theme.Accent: gold, theme.Selected: copper,
		theme.Success: verdigris, theme.Warning: gold, theme.Error: cinnabar,
		theme.Keyword: amethyst, theme.Operator: amethyst, theme.Literal: copper, theme.Function: gold,
		theme.Alias: parchment, theme.Parameter: copper, theme.String: verdigris, theme.Number: copper,
		theme.Comment: ash, theme.Punctuation: ash,
	}
	for _, role := range theme.Roles() {
		got := theme.Default().Color(role)
		assert.Equal(t, want[role], got, role.String())
		assert.NotEqual(t, got.Light, got.Dark, "%s adapts to light and dark", role)
	}
	assert.True(t, theme.Default().Adapts())
}

func TestRedIsAnOriginalDarkTheme(t *testing.T) {
	red := mustFindTheme(t, "red")

	assert.False(t, red.Adapts())
	assert.Equal(t, theme.About{
		Title: "Red", Author: "Alchemist", Source: "https://github.com/colbytimm/Alchemist",
		License: "MIT", Background: "#3f141c",
	}, red.About())
}

func TestFindReadsACustomTheme(t *testing.T) {
	custom := customThemes(map[string]string{"mine.toml": withColor(alchemistFile(t), theme.Keyword, `"#FF0000"`)})

	got, err := theme.Find("mine", custom)

	require.NoError(t, err)
	assert.Equal(t, "mine", got.Name())
	assert.Equal(t, "#FF0000", got.Color(theme.Keyword).Dark)
}

func TestFindRefusesAnUnknownNameAndListsBothKinds(t *testing.T) {
	custom := customThemes(map[string]string{"mine.toml": alchemistFile(t)})

	_, err := theme.Find("drakula", custom)

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
	assert.Equal(t, `theme "drakula": unknown theme: built-in: `+strings.Join(theme.BuiltinNames(), ", ")+
		"; custom: mine (in "+customDir+")", err.Error())
}

func TestFindRefusesANameThatCannotBeAFile(t *testing.T) {
	custom := customThemes(map[string]string{"my theme.toml": alchemistFile(t)})

	_, err := theme.Find("my theme", custom)

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
}

func TestFindNamesTheFileOfABrokenTheme(t *testing.T) {
	custom := customThemes(map[string]string{"mine.toml": withColor(alchemistFile(t), theme.Keyword, `"#12345"`)})

	_, err := theme.Find("mine", custom)

	require.ErrorIs(t, err, theme.ErrInvalidTheme)
	assert.Equal(t, `theme mine: `+customDir+`/mine.toml: keyword: "#12345" is not #RRGGBB`, err.Error())
}

func TestABuiltinNameReachesTheBuiltinTheme(t *testing.T) {
	custom := customThemes(map[string]string{theme.DefaultName + ".toml": withColor(alchemistFile(t), theme.Keyword, `"#FF0000"`)})

	got, err := theme.Find(theme.DefaultName, custom)

	require.NoError(t, err)
	assert.Equal(t, theme.Default(), got)
}

func TestFileIsTheEmbeddedBytes(t *testing.T) {
	got, err := theme.File(theme.DefaultName, theme.Custom{})

	require.NoError(t, err)
	assert.Contains(t, string(got), "# Alchemist's default theme.")
}

func TestFileOfAnUnknownThemeIsRefused(t *testing.T) {
	_, err := theme.File("nope", theme.Custom{})

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
}

func TestListShowsBuiltinsThenCustomThemes(t *testing.T) {
	custom := customThemes(map[string]string{
		"broken.toml":    withColor(alchemistFile(t), theme.Keyword, `"#12345"`),
		"mine.toml":      alchemistFile(t),
		"notes.txt":      "not a theme",
		"alchemist.toml": alchemistFile(t),
	})

	entries := theme.List(custom)

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	want := append(theme.BuiltinNames(), theme.DefaultName, "broken", "mine", "notes.txt")
	require.Equal(t, want, names)
	byName := func(i int) theme.Entry { return entries[len(theme.BuiltinNames())+i] }
	assert.EqualError(t, byName(0).Err, "theme alchemist: "+customDir+"/alchemist.toml: alchemist is a built-in theme: rename the file")
	assert.ErrorIs(t, byName(1).Err, theme.ErrInvalidTheme)
	assert.NoError(t, byName(2).Err)
	assert.Equal(t, "Alchemist", byName(2).About.Title)
	assert.True(t, byName(3).Ignored)
	for _, entry := range entries[:len(theme.BuiltinNames())] {
		assert.True(t, entry.BuiltIn)
		assert.NoError(t, entry.Err)
	}
}

func TestListWithoutACustomFolderIsTheBuiltins(t *testing.T) {
	assert.Len(t, theme.List(theme.Custom{Dir: customDir, Files: os.DirFS(t.TempDir() + "/missing")}), len(theme.BuiltinNames()))
}
