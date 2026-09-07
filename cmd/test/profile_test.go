package cmd_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/config"
)

const enteredKey = "entered-key-material"

func TestProfileAddStoresTheKeyAndListsOnlyItsSource(t *testing.T) {
	h := newHarness(t)

	added, err := h.run(enteredKey+"\n", "profile", "add", "emulator",
		"--endpoint", "https://localhost:8081", "--insecure-skip-verify")
	require.NoError(t, err)
	listed, err := h.run("", "profile", "list")
	require.NoError(t, err)

	assert.Contains(t, added, "key stored")
	assert.Equal(t, enteredKey, h.keyring.secrets["emulator"])
	assert.Contains(t, listed, "emulator (default)")
	assert.Contains(t, listed, "https://localhost:8081")
	assert.Contains(t, listed, config.SourceKeychain)
	assert.NotContains(t, listed, enteredKey)
	assert.NotContains(t, added, enteredKey)
}

func TestProfileAddWithoutAKeyNamesTheFallbacks(t *testing.T) {
	h := newHarness(t)

	out, err := h.run("\n", "profile", "add", "emulator", "--endpoint", "https://localhost:8081")
	require.NoError(t, err)

	assert.Contains(t, out, "ALCHEMIST_EMULATOR_KEY")
	assert.Contains(t, out, "set-key")
	assert.Empty(t, h.keyring.secrets)
	listed, err := h.run("", "profile", "list")
	require.NoError(t, err)
	assert.Contains(t, listed, "none")
}

func TestProfileAddWritesNothingForAnUnknownAdapter(t *testing.T) {
	h := newHarness(t)

	_, err := h.run("", "profile", "add", "x", "--endpoint", "https://x", "--adapter", "postgres")

	require.ErrorIs(t, err, adapter.ErrUnknownAdapter)
	_, statErr := os.Stat(configPath(t))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProfileAddRequiresAnEndpoint(t *testing.T) {
	_, err := newHarness(t).run("", "profile", "add", "x")

	require.ErrorIs(t, err, config.ErrInvalidConfig)
}

func TestProfileAddRejectsADuplicate(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "profile", "add", "emulator", "--endpoint", "https://elsewhere")

	require.ErrorIs(t, err, config.ErrProfileExists)
}

func TestProfileAddDefaultFlagMovesTheDefault(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")
	h.addProfile(t, "prod", "--default")

	listed, err := h.run("", "profile", "list")
	require.NoError(t, err)

	assert.Contains(t, listed, "prod (default)")
	assert.NotContains(t, listed, "emulator (default)")
}

func TestProfileAddKeepsTheOptionalFields(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator", "--database", "sales", "--page-size", "25")

	store, err := config.DefaultStore()
	require.NoError(t, err)
	cfg, err := store.Load()
	require.NoError(t, err)

	assert.Equal(t, "sales", cfg.Profiles["emulator"].Database)
	assert.Equal(t, 25, cfg.Profiles["emulator"].PageSize)
}

func TestProfileSetKeyReplacesTheKey(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	out, err := h.run(enteredKey+"\n", "profile", "set-key", "emulator")
	require.NoError(t, err)

	assert.Equal(t, enteredKey, h.keyring.secrets["emulator"])
	assert.NotContains(t, out, enteredKey)
}

func TestProfileSetKeyRejectsAnEmptyAnswer(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("\n", "profile", "set-key", "emulator")

	require.Error(t, err)
	assert.Empty(t, h.keyring.secrets)
}

func TestProfileSetKeyUnknownProfile(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run(enteredKey+"\n", "profile", "set-key", "staging")

	require.ErrorIs(t, err, config.ErrProfileNotFound)
	assert.Empty(t, h.keyring.secrets)
}

func TestProfileRemoveDeletesTheEntryAndItsKey(t *testing.T) {
	h := newHarness(t)
	_, err := h.run(enteredKey+"\n", "profile", "add", "emulator", "--endpoint", "https://localhost:8081")
	require.NoError(t, err)

	_, err = h.run("", "profile", "remove", "emulator")
	require.NoError(t, err)

	assert.Empty(t, h.keyring.secrets)
	listed, err := h.run("", "profile", "list")
	require.NoError(t, err)
	assert.Contains(t, listed, "no profiles")
}

func TestProfileRemoveSurvivesAMissingKeychainEntry(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "profile", "remove", "emulator")

	require.NoError(t, err)
}

func TestProfileRemoveUnknownProfile(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "profile", "remove", "staging")

	require.ErrorIs(t, err, config.ErrProfileNotFound)
}

func TestProfileListShowsAnEnvironmentSource(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "my-emulator")
	t.Setenv("ALCHEMIST_MY_EMULATOR_KEY", enteredKey)

	listed, err := h.run("", "profile", "list")
	require.NoError(t, err)

	assert.Contains(t, listed, "$ALCHEMIST_MY_EMULATOR_KEY")
	assert.NotContains(t, listed, enteredKey)
}

func TestProfileListWithNoConfigSaysHowToAdd(t *testing.T) {
	out, err := newHarness(t).run("", "profile", "list")

	require.NoError(t, err)
	assert.Contains(t, out, "profile add")
	assert.Contains(t, out, configPath(t))
}

func TestProfileWithoutASubcommandPrintsHelp(t *testing.T) {
	out, err := newHarness(t).run("", "profile")

	require.NoError(t, err)
	assert.Contains(t, out, "set-key")
}
