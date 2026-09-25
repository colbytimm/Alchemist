package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/saved"
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
	h.addProfile(t, "emulator", "--database", "sales", "--page-size", "25", "--max-join-rows", "500", "--writers", "8")

	store, err := config.DefaultStore()
	require.NoError(t, err)
	cfg, err := store.Load()
	require.NoError(t, err)

	assert.Equal(t, "sales", cfg.Profiles["emulator"].Database)
	assert.Equal(t, 25, cfg.Profiles["emulator"].PageSize)
	assert.Equal(t, 500, cfg.Profiles["emulator"].MaxJoinRows)
	assert.Equal(t, 8, cfg.Profiles["emulator"].Writers)
	accounts, err := h.profiles(t).Accounts()
	require.NoError(t, err)
	assert.Equal(t, 8, accounts[0].Writers, "the account carries it to the session")
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

// saveQueries files queries for account where a session would have saved
// them, under the harness's config directory.
func saveQueries(t *testing.T, account string, names ...string) saved.Dir {
	t.Helper()
	dir, err := config.Dir()
	require.NoError(t, err)
	queries := saved.Open(filepath.Join(dir, saved.DirName))
	for _, name := range names {
		require.NoError(t, queries.Create(account, saved.Query{Name: name, Text: "SELECT * FROM c"}))
	}
	return queries
}

func TestProfileRemoveKeepsTheSavedQueriesAndSaysWhere(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "staging")
	queries := saveQueries(t, "staging", "open orders", "late shipments")

	out, err := h.run("", "profile", "remove", "staging")

	require.NoError(t, err)
	assert.Contains(t, out, "kept 2 saved queries in "+queries.AccountPath("staging"))
	assert.Contains(t, out, "alchemist profile remove staging --purge")
	assert.DirExists(t, queries.AccountPath("staging"))
}

func TestProfileRemovePurgeDeletesTheSavedQueries(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "staging")
	queries := saveQueries(t, "staging", "open orders")

	out, err := h.run("", "profile", "remove", "staging", "--purge")

	require.NoError(t, err)
	assert.Contains(t, out, "removed profile staging and its 1 saved query")
	assert.NoDirExists(t, queries.AccountPath("staging"))
}

func TestProfileRemoveWithNothingSavedDoesNotMentionQueries(t *testing.T) {
	for _, args := range [][]string{{"profile", "remove", "staging"}, {"profile", "remove", "staging", "--purge"}} {
		h := newHarness(t)
		h.addProfile(t, "staging")

		out, err := h.run("", args...)

		require.NoError(t, err)
		assert.Equal(t, "removed profile staging\n", out, "%v", args)
	}
}

func TestProfileRemoveLeavesOtherAccountsQueries(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "staging")
	h.addProfile(t, "prod")
	queries := saveQueries(t, "prod", "open orders")
	saveQueries(t, "staging", "open orders")

	_, err := h.run("", "profile", "remove", "staging", "--purge")

	require.NoError(t, err)
	assert.DirExists(t, queries.AccountPath("prod"))
}

func TestProfileRemoveCountsFilesTheOverlaySkips(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "staging")
	queries := saveQueries(t, "staging")
	require.NoError(t, os.MkdirAll(queries.AccountPath("staging"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(queries.AccountPath("staging"), "empty.sql"), nil, 0o600))

	out, err := h.run("", "profile", "remove", "staging")

	require.NoError(t, err)
	assert.Contains(t, out, "kept 1 saved query in "+queries.AccountPath("staging"))
}

func readOnlyOf(t *testing.T, h harness, name string) bool {
	t.Helper()
	accounts, err := h.profiles(t).Accounts()
	require.NoError(t, err)
	for _, account := range accounts {
		if account.Name == name {
			return account.ReadOnly
		}
	}
	require.Failf(t, "no such account", "%s", name)
	return false
}

func TestAProfileIsReadOnlyUnlessItsEndpointIsLocal(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")
	_, err := h.run("", "profile", "add", "prod", "--endpoint", "https://myaccount.documents.azure.com:443/")
	require.NoError(t, err)

	assert.False(t, readOnlyOf(t, h, "emulator"))
	assert.True(t, readOnlyOf(t, h, "prod"))
}

func TestProfileAddReadOnlyFalseAllowsWritesOnARemoteAccount(t *testing.T) {
	h := newHarness(t)

	_, err := h.run("", "profile", "add", "prod", "--endpoint", "https://myaccount.documents.azure.com:443/", "--read-only=false")

	require.NoError(t, err)
	assert.False(t, readOnlyOf(t, h, "prod"))
}

func TestProfileSetReadOnly(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	out, err := h.run("", "profile", "set-read-only", "emulator", "true")
	require.NoError(t, err)
	assert.Contains(t, out, "profile emulator is read-only")
	assert.True(t, readOnlyOf(t, h, "emulator"))

	out, err = h.run("", "profile", "set-read-only", "emulator", "false")
	require.NoError(t, err)
	assert.Contains(t, out, "profile emulator allows writes")
	assert.False(t, readOnlyOf(t, h, "emulator"))
}

func TestProfileSetReadOnlyRefusesAnythingButABool(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "profile", "set-read-only", "emulator", "maybe")

	require.ErrorContains(t, err, "give true or false")
}

func TestProfileSetReadOnlyUnknownProfile(t *testing.T) {
	_, err := newHarness(t).run("", "profile", "set-read-only", "ghost", "false")

	require.ErrorIs(t, err, config.ErrNoProfiles)
}
