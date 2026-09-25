//go:build integration

package integration

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/history"
	"github.com/colbytimm/alchemist/internal/theme"
	"github.com/colbytimm/alchemist/internal/tui"
)

// TestIntegrationAccounts is the manual checklist of
// docs/plan/14-multiple-accounts.md: two profiles on the emulator are two
// accounts with a history each, and a third that cannot connect leaves the
// session where it was.
func TestIntegrationAccounts(t *testing.T) {
	connectWithRetry(t)
	freshFixture(t)
	profiles := emulatorProfiles(t)
	accounts, err := profiles.Accounts()
	require.NoError(t, err)
	store, err := history.Open(t.TempDir())
	require.NoError(t, err)

	m, _ := tui.New(tui.Options{
		Icons:    theme.Icons(),
		Accounts: accounts,
		Launch:   "alpha",
		Open:     profiles.Open,
		Connect:  profiles.Connect,
		History:  store,
	}).Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m = settle(m, m.Init())
	require.Contains(t, view(m), fixtureContainer, "alpha opens on its database")

	m = runQuery(t, press(t, m, keyMsg(tea.KeyDown), keyMsg(tea.KeyEnter)), "SELECT * FROM c")
	require.Contains(t, view(m), "Results · alpha")

	m = switchAccount(t, m, 1)
	require.True(t, strings.HasPrefix(lastLine(m), "beta "), "the session is on beta")
	m = runAnother(t, m, "SELECT * FROM "+fixtureDatabase+"."+fixtureContainer+" c")
	require.Contains(t, view(m), "Results · beta")

	betaRuns, err := store.Recent("beta", 10)
	require.NoError(t, err)
	assert.Len(t, betaRuns, 1)
	alphaRuns, err := store.Recent("alpha", 10)
	require.NoError(t, err)
	assert.Len(t, alphaRuns, 1)

	m = switchAccount(t, m, 2)
	assert.Contains(t, view(m), "failed", "the closed port fails in the switcher")
	assert.True(t, strings.HasPrefix(lastLine(press(t, m, keyMsg(tea.KeyEscape))), "beta "),
		"and the session stays where it was")
}

// emulatorProfiles writes three profiles: two on the emulator, one on a port
// nothing listens on.
func emulatorProfiles(t *testing.T) cmd.Profiles {
	t.Helper()
	if err := cmd.RegisterAdapters(); err != nil && !errors.Is(err, adapter.ErrDuplicateName) {
		require.NoError(t, err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := config.DefaultStore()
	require.NoError(t, err)
	raw := settings()
	cfg := config.Config{}
	for _, profile := range []config.Profile{
		{Name: "alpha", Endpoint: raw["endpoint"], Database: fixtureDatabase},
		{Name: "beta", Endpoint: raw["endpoint"]},
		{Name: "closed", Endpoint: "http://localhost:1"},
	} {
		profile.Adapter, profile.InsecureSkipVerify = cosmos.Name, true
		cfg, err = cfg.Add(profile)
		require.NoError(t, err)
		t.Setenv(config.EnvKeyVar(profile.Name), raw["key"])
	}
	require.NoError(t, store.Save(cfg))
	return cmd.Profiles{Store: store, Keyring: emptyKeyring{}}
}

// switchAccount picks the row-th account in the switcher, in name order.
func switchAccount(t *testing.T, m tea.Model, row int) tea.Model {
	t.Helper()
	m = press(t, m, keyMsg(tea.KeyCtrlG))
	for range 3 {
		m = press(t, m, keyMsg(tea.KeyUp))
	}
	for range row {
		m = press(t, m, keyMsg(tea.KeyDown))
	}
	return press(t, m, keyMsg(tea.KeyEnter))
}

func lastLine(m tea.Model) string {
	lines := strings.Split(view(m), "\n")
	return lines[len(lines)-1]
}
