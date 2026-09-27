package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/logging"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	dracula = "dracula-at-midnight"
	jarvis  = "jarvis-hud"
)

func themesDir() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "alchemist", theme.DirName)
}

// writeCustomTheme files a custom theme called name, from alchemist's file
// with its keyword set to keyword.
func writeCustomTheme(t *testing.T, name, keyword string) {
	t.Helper()
	data, err := theme.File(theme.DefaultName, theme.Custom{})
	require.NoError(t, err)
	file := strings.Replace(string(data), `keyword = { light = "#6C3FA0", dark = "#9B6FD0" }`, `keyword = "`+keyword+`"`, 1)
	require.NoError(t, os.MkdirAll(themesDir(), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(themesDir(), name+".toml"), []byte(file), 0o600))
}

func savedTheme(t *testing.T) string {
	t.Helper()
	cfg, err := config.Store{Path: configPath(t)}.Load()
	require.NoError(t, err)
	return cfg.Theme
}

func activeTheme() string {
	return theme.Active().Theme().Name()
}

// launchMock starts a session on the mock adapter, with args besides.
func (h harness) launchMock(t *testing.T, args ...string) {
	t.Helper()
	require.ErrorIs(t, h.launch(append([]string{"--adapter", mock.Name}, args...)...), tea.ErrProgramKilled)
}

func logged(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(statePath(logging.FileName))
	require.NoError(t, err)
	return string(text)
}

func TestWithNothingSavedALaunchIsAlchemist(t *testing.T) {
	h := newHarness(t)
	theme.Use(mustFind(t, jarvis))

	h.launchMock(t)

	assert.Equal(t, theme.DefaultName, activeTheme())
}

func TestThemeUseSticksAcrossLaunchesUntilChanged(t *testing.T) {
	h := newHarness(t)

	out, err := h.run("", "theme", "use", dracula)

	require.NoError(t, err)
	assert.Equal(t, "theme "+dracula+" saved: alchemist opens in it from now on\n", out)
	assert.Equal(t, dracula, savedTheme(t))
	for range 2 {
		theme.Use(theme.Default())
		h.launchMock(t)
		assert.Equal(t, dracula, activeTheme())
	}
}

func TestTheCatalogSavesAsThemeUseDoes(t *testing.T) {
	h := newHarness(t)

	require.NoError(t, h.themes(t).Save(dracula))

	assert.Equal(t, dracula, h.themes(t).Saved())
	h.launchMock(t)
	assert.Equal(t, dracula, activeTheme())
}

func (h harness) themes(t *testing.T) cmd.Themes {
	t.Helper()
	store, err := config.DefaultStore()
	require.NoError(t, err)
	return cmd.Themes{Store: store, Custom: theme.Custom{Dir: themesDir(), Files: os.DirFS(themesDir())}}
}

func TestThemeFlagBeatsTheSavedThemeForOneLaunch(t *testing.T) {
	h := newHarness(t)
	_, err := h.run("", "theme", "use", dracula)
	require.NoError(t, err)

	h.launchMock(t, "--theme", jarvis)

	assert.Equal(t, jarvis, activeTheme())
	assert.Equal(t, dracula, savedTheme(t), "--theme never touches the saved theme")
}

func TestThemeUseKeepsEveryProfile(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")
	h.addProfile(t, "staging", "--default")
	before, err := config.Store{Path: configPath(t)}.Load()
	require.NoError(t, err)

	_, err = h.run("", "theme", "use", jarvis)
	require.NoError(t, err)

	after, err := config.Store{Path: configPath(t)}.Load()
	require.NoError(t, err)
	before.Theme = jarvis
	assert.Equal(t, before, after)
}

func TestEveryOtherWriterKeepsTheSavedTheme(t *testing.T) {
	writers := []struct {
		name  string
		write func(t *testing.T, h harness)
	}{
		{name: "profile add", write: func(t *testing.T, h harness) { h.addProfile(t, "staging") }},
		{name: "profile remove", write: func(t *testing.T, h harness) {
			_, err := h.run("", "profile", "remove", "emulator")
			require.NoError(t, err)
		}},
		{name: "profile set-key", write: func(t *testing.T, h harness) {
			_, err := h.run(enteredKey+"\n", "profile", "set-key", "emulator")
			require.NoError(t, err)
		}},
		{name: "profile set-read-only", write: func(t *testing.T, h harness) {
			_, err := h.run("", "profile", "set-read-only", "emulator", "false")
			require.NoError(t, err)
		}},
		{name: "the connect form", write: func(t *testing.T, h harness) {
			connectForm(t, h, "dev", "https://localhost:8081")
		}},
	}
	for _, tt := range writers {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.addProfile(t, "emulator")
			h.addProfile(t, "dev", "--adapter", mock.Name)
			_, err := h.run("", "theme", "use", jarvis)
			require.NoError(t, err)

			tt.write(t, h)

			assert.Equal(t, jarvis, savedTheme(t))
		})
	}
}

func TestThemeUseRefusesAnUnknownThemeAndTouchesNothing(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")
	before, err := os.ReadFile(configPath(t))
	require.NoError(t, err)

	_, err = h.run("", "theme", "use", "drakula")

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
	assert.Contains(t, err.Error(), dracula, "the message lists what exists")
	after, err := os.ReadFile(configPath(t))
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestThemeUseRefusesABrokenCustomTheme(t *testing.T) {
	h := newHarness(t)
	writeCustomTheme(t, "mine", "#12345")

	_, err := h.run("", "theme", "use", "mine")

	require.ErrorIs(t, err, theme.ErrInvalidTheme)
	assert.Contains(t, err.Error(), `keyword: "#12345" is not #RRGGBB`)
	assert.NoFileExists(t, configPath(t))
}

func TestThemeUseWithNoConfigWritesOnlyTheTheme(t *testing.T) {
	h := newHarness(t)

	_, err := h.run("", "theme", "use", jarvis)
	require.NoError(t, err)

	text, err := os.ReadFile(configPath(t))
	require.NoError(t, err)
	assert.Equal(t, "theme = \""+jarvis+"\"\n", string(text))
}

func TestAnUnknownThemeFlagRefusesToStartAndLeavesNoLog(t *testing.T) {
	h := newHarness(t)

	_, err := h.run("", "--adapter", mock.Name, "--theme", "nope")

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
	assert.Contains(t, err.Error(), dracula)
	assert.NoDirExists(t, filepath.Join(os.Getenv("XDG_STATE_HOME"), "alchemist"))
}

func TestAnUnknownSavedThemeOpensInAlchemistAndSaysWhy(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, config.Store{Path: configPath(t)}.Save(config.Config{Theme: "nope"}))
	theme.Use(mustFind(t, jarvis))

	h.launchMock(t)

	assert.Equal(t, theme.DefaultName, activeTheme())
	assert.Contains(t, logged(t), "nope")
	assert.Equal(t, "nope", savedTheme(t), "the setting is kept")
}

func TestADeletedCustomThemeFallsBackUntilItIsRestored(t *testing.T) {
	h := newHarness(t)
	writeCustomTheme(t, "mine", "#FF0000")
	_, err := h.run("", "theme", "use", "mine")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(themesDir(), "mine.toml")))

	h.launchMock(t)
	assert.Equal(t, theme.DefaultName, activeTheme())
	assert.Equal(t, "mine", savedTheme(t))

	writeCustomTheme(t, "mine", "#FF0000")
	h.launchMock(t)
	assert.Equal(t, "mine", activeTheme())
}

func TestACustomThemeIsFoundEverywhere(t *testing.T) {
	h := newHarness(t)
	writeCustomTheme(t, "mine", "#FF0000")

	h.launchMock(t, "--theme", "mine")
	assert.Equal(t, "mine", activeTheme(), "by --theme")

	theme.Use(theme.Default())
	_, err := h.run("", "theme", "use", "mine")
	require.NoError(t, err)
	h.launchMock(t)
	assert.Equal(t, "mine", activeTheme(), "by the saved theme")

	var names []string
	for _, entry := range h.themes(t).List() {
		names = append(names, entry.Name)
	}
	assert.Contains(t, names, "mine", "by the picker's list")
}

func TestAProfileLaunchOpensInTheSavedTheme(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	t.Setenv(config.EnvKeyVar("dev"), "from-env")
	_, err := h.run("", "theme", "use", jarvis)
	require.NoError(t, err)

	require.ErrorIs(t, h.launch("dev"), tea.ErrProgramKilled)

	assert.Equal(t, jarvis, activeTheme())
}

func TestThemeListNamesEveryThemeAndMarksTheSavedOne(t *testing.T) {
	h := newHarness(t)
	writeCustomTheme(t, "mine", "#FF0000")
	writeCustomTheme(t, "broken", "#12345")
	_, err := h.run("", "theme", "use", jarvis)
	require.NoError(t, err)

	out, err := h.run("", "theme", "list")

	require.NoError(t, err)
	for _, name := range theme.BuiltinNames() {
		assert.Contains(t, out, name)
	}
	assert.Contains(t, out, "mine")
	assert.Contains(t, out, `keyword: "#12345" is not #RRGGBB`)
	assert.Regexp(t, `(?m)^\* `+jarvis+`\b`, out)
}

func TestThemeShowPrintsAFileThatParsesToTheBuiltin(t *testing.T) {
	out, err := newHarness(t).run("", "theme", "show", dracula)
	require.NoError(t, err)

	parsed, err := theme.Parse(dracula, []byte(out))

	require.NoError(t, err)
	assert.Equal(t, mustFind(t, dracula), parsed)
	assert.Contains(t, out, "Copyright (c) 2025 Wallacy Santos Ferreira")
}

func TestThemeShowOfAnUnknownThemeFails(t *testing.T) {
	_, err := newHarness(t).run("", "theme", "show", "nope")

	require.ErrorIs(t, err, theme.ErrUnknownTheme)
}

func mustFind(t *testing.T, name string) theme.Theme {
	t.Helper()
	found, err := theme.Find(name, theme.Custom{})
	require.NoError(t, err)
	return found
}
