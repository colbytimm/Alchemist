package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/config"
)

func tempStore(t *testing.T) config.Store {
	t.Helper()
	return config.Store{Path: filepath.Join(t.TempDir(), "alchemist", config.FileName)}
}

func writeConfig(t *testing.T, store config.Store, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(store.Path), 0o700))
	require.NoError(t, os.WriteFile(store.Path, []byte(text), 0o600))
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	prod := prodProfile()
	prod.Database = "sales"
	prod.PageSize = 50
	prod.MaxJoinRows = 2000
	want, err := config.Config{}.Add(emulatorProfile())
	require.NoError(t, err)
	want, err = want.Add(prod)
	require.NoError(t, err)
	store := tempStore(t)

	require.NoError(t, store.Save(want))
	got, err := store.Load()
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestSaveOmitsUnsetOptionalFields(t *testing.T) {
	store := tempStore(t)
	cfg, err := config.Config{}.Add(prodProfile())
	require.NoError(t, err)

	require.NoError(t, store.Save(cfg))

	text, err := os.ReadFile(store.Path)
	require.NoError(t, err)
	for _, unset := range []string{"page_size", "max_join_rows", "writers", "database", "insecure_skip_verify", "snapshot", "max_mutation_items"} {
		assert.NotContains(t, string(text), unset)
	}
}

func TestSaveCreatesAPrivateFile(t *testing.T) {
	store := tempStore(t)

	require.NoError(t, store.Save(twoProfiles(t)))

	info, err := os.Stat(store.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestLoadMissingFileIsAnEmptyConfig(t *testing.T) {
	cfg, err := tempStore(t).Load()

	require.NoError(t, err)
	assert.Empty(t, cfg.Names())
}

func TestLoadParsesTheDocumentedExample(t *testing.T) {
	store := tempStore(t)
	writeConfig(t, store, `
default_profile = "emulator"
snapshot_dir = "/mnt/big/snapshots"

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # optional default scope
page_size = 100
max_join_rows = 5000
writers = 8
snapshot_max_items = 10000000
max_mutation_items = 50000

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
`)

	cfg, err := store.Load()
	require.NoError(t, err)

	profile, err := cfg.Profile("")
	require.NoError(t, err)
	assert.Equal(t, config.Profile{
		Name:               "emulator",
		Adapter:            "cosmos",
		Endpoint:           "https://localhost:8081",
		InsecureSkipVerify: true,
		Database:           "sales",
		PageSize:           100,
		MaxJoinRows:        5000,
		Writers:            8,
		SnapshotMaxItems:   10000000,
		MaxMutationItems:   50000,
	}, profile)
	assert.Equal(t, "/mnt/big/snapshots", cfg.SnapshotDir)
	assert.Equal(t, []string{"emulator", "prod"}, cfg.Names())
}

func TestSaveWritesTheWellKnownKeySettingAndNoKey(t *testing.T) {
	store := tempStore(t)
	want, err := config.Config{}.Add(config.Profile{
		Name: "emulator", Adapter: "cosmos", Endpoint: "http://localhost:8081", WellKnownKey: true,
	})
	require.NoError(t, err)

	require.NoError(t, store.Save(want))

	text, err := os.ReadFile(store.Path)
	require.NoError(t, err)
	assert.Contains(t, string(text), "well_known_key = true")
	assert.NotContains(t, string(text), config.EmulatorKey)
	got, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLoadRejectsBrokenFilesByName(t *testing.T) {
	const validProfile = "[profiles.a]\nadapter = \"cosmos\"\nendpoint = \"https://x\"\n"
	tests := []struct {
		name    string
		text    string
		wantErr error // nil when only the TOML parser can name the problem
	}{
		{name: "bad toml", text: "default_profile = \n"},
		{name: "duplicate profile", text: validProfile + validProfile},
		{name: "unknown key", text: "[profiles.a]\nadapter = \"cosmos\"\naddress = \"https://x\"\n", wantErr: config.ErrInvalidConfig},
		{name: "missing endpoint", text: "[profiles.a]\nadapter = \"cosmos\"\n", wantErr: config.ErrInvalidConfig},
		{name: "missing adapter", text: "[profiles.a]\nendpoint = \"https://x\"\n", wantErr: config.ErrInvalidConfig},
		{name: "default names a missing profile", text: "default_profile = \"b\"\n" + validProfile, wantErr: config.ErrProfileNotFound},
		{
			name:    "well-known key off this machine",
			text:    "[profiles.a]\nadapter = \"cosmos\"\nendpoint = \"https://x.documents.azure.com:443/\"\nwell_known_key = true\n",
			wantErr: config.ErrInvalidConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tempStore(t)
			writeConfig(t, store, tt.text)

			_, err := store.Load()

			require.Error(t, err)
			assert.Contains(t, err.Error(), store.Path, "the message must say which file")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestDefaultStoreHonorsXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	store, err := config.DefaultStore()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(base, "alchemist", config.FileName), store.Path)
}

func TestDefaultStoreFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	store, err := config.DefaultStore()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".config", "alchemist", config.FileName), store.Path)
}

func TestSaveProfileReplacesTheProfileAndFilesTheKey(t *testing.T) {
	store := tempStore(t)
	keyring := newFakeKeyring()
	require.NoError(t, config.SaveProfile(store, keyring, emulatorProfile(), "first-key"))
	changed := emulatorProfile()
	changed.Endpoint = "https://elsewhere:8081"

	require.NoError(t, config.SaveProfile(store, keyring, changed, "second-key"))

	cfg, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://elsewhere:8081", cfg.Profiles["emulator"].Endpoint)
	assert.Equal(t, "emulator", cfg.DefaultProfile)
	assert.Equal(t, "second-key", keyring.secrets["emulator"])
}

func TestSaveProfileWithoutAKeyLeavesTheKeyringAlone(t *testing.T) {
	keyring := newFakeKeyring()

	require.NoError(t, config.SaveProfile(tempStore(t), keyring, emulatorProfile(), ""))

	assert.Empty(t, keyring.secrets)
}

func TestSaveProfileSavesNothingWhenTheKeychainRefuses(t *testing.T) {
	store := tempStore(t)

	err := config.SaveProfile(store, &fakeKeyring{err: errNoKeychain}, emulatorProfile(), "key")

	require.ErrorIs(t, err, errNoKeychain)
	cfg, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.Empty(t, cfg.Profiles)
}

func TestSaveProfileFilesNoKeyForAProfileTheConfigRefuses(t *testing.T) {
	store := tempStore(t)
	keyring := newFakeKeyring()
	require.NoError(t, config.SaveProfile(store, keyring, prodProfile(), "prod-key"))
	shouting := prodProfile()
	shouting.Name = "Prod"

	err := config.SaveProfile(store, keyring, shouting, "other-key")

	require.ErrorIs(t, err, config.ErrProfileExists)
	assert.Equal(t, map[string]string{"prod": "prod-key"}, keyring.secrets)
}

// A file written before names were kept distinct by case still loads, so the
// profile commands that could tidy it up are not locked out.
func TestLoadAcceptsNamesDifferingOnlyInCase(t *testing.T) {
	store := tempStore(t)
	writeConfig(t, store, "[profiles.a]\nadapter = \"cosmos\"\nendpoint = \"https://x\"\n"+
		"[profiles.A]\nadapter = \"cosmos\"\nendpoint = \"https://x\"\n")

	cfg, err := store.Load()

	require.NoError(t, err)
	assert.Equal(t, []string{"A", "a"}, cfg.Names())
}
