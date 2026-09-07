package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/config"
)

func emulatorProfile() config.Profile {
	return config.Profile{
		Name:               "emulator",
		Adapter:            "cosmos",
		Endpoint:           "https://localhost:8081",
		InsecureSkipVerify: true,
	}
}

func prodProfile() config.Profile {
	return config.Profile{
		Name:     "prod",
		Adapter:  "cosmos",
		Endpoint: "https://myaccount.documents.azure.com:443/",
	}
}

func twoProfiles(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Config{}.Add(emulatorProfile())
	require.NoError(t, err)
	cfg, err = cfg.Add(prodProfile())
	require.NoError(t, err)
	return cfg
}

func TestFirstProfileAddedBecomesTheDefault(t *testing.T) {
	cfg := twoProfiles(t)

	assert.Equal(t, "emulator", cfg.DefaultProfile)
	assert.Equal(t, []string{"emulator", "prod"}, cfg.Names())
}

func TestProfileLookup(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		lookup  string
		want    string
		wantErr error
	}{
		{name: "named profile", cfg: twoProfiles(t), lookup: "prod", want: "prod"},
		{name: "empty name means the default", cfg: twoProfiles(t), lookup: "", want: "emulator"},
		{name: "unknown name", cfg: twoProfiles(t), lookup: "staging", wantErr: config.ErrProfileNotFound},
		{name: "no profiles at all", cfg: config.Config{}, lookup: "", wantErr: config.ErrNoProfiles},
		{
			name:    "profiles but no default",
			cfg:     config.Config{Profiles: map[string]config.Profile{"prod": prodProfile()}},
			lookup:  "",
			wantErr: config.ErrNoDefaultProfile,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.Profile(tt.lookup)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestAddRejectsWhatCannotBeLaunched(t *testing.T) {
	tests := []struct {
		name    string
		profile config.Profile
		wantErr error
	}{
		{name: "duplicate name", profile: emulatorProfile(), wantErr: config.ErrProfileExists},
		{
			name:    "name with a space",
			profile: config.Profile{Name: "my emulator", Adapter: "cosmos", Endpoint: "https://x"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "missing adapter",
			profile: config.Profile{Name: "x", Endpoint: "https://x"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "missing endpoint",
			profile: config.Profile{Name: "x", Adapter: "cosmos"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "negative page size",
			profile: config.Profile{Name: "x", Adapter: "cosmos", Endpoint: "https://x", PageSize: -1},
			wantErr: config.ErrInvalidConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := twoProfiles(t).Add(tt.profile)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestAddLeavesTheOriginalUntouched(t *testing.T) {
	original := twoProfiles(t)

	_, err := original.Add(config.Profile{Name: "staging", Adapter: "cosmos", Endpoint: "https://x"})
	require.NoError(t, err)

	assert.Equal(t, []string{"emulator", "prod"}, original.Names())
}

func TestRemoveClearsADefaultThatPointedAtIt(t *testing.T) {
	cfg, err := twoProfiles(t).Remove("emulator")
	require.NoError(t, err)

	assert.Equal(t, []string{"prod"}, cfg.Names())
	assert.Empty(t, cfg.DefaultProfile)
}

func TestRemoveKeepsAnUnrelatedDefault(t *testing.T) {
	cfg, err := twoProfiles(t).Remove("prod")
	require.NoError(t, err)

	assert.Equal(t, "emulator", cfg.DefaultProfile)
}

func TestRemoveUnknownName(t *testing.T) {
	_, err := twoProfiles(t).Remove("staging")

	require.ErrorIs(t, err, config.ErrProfileNotFound)
}

func TestSettingsCarryTheSecretUnderItsOwnKey(t *testing.T) {
	profile := emulatorProfile()

	fromKey := profile.Settings(config.Secret{Key: "k"})
	assert.Equal(t, "k", fromKey["key"])
	assert.Empty(t, fromKey["connection_string"])
	assert.Equal(t, "https://localhost:8081", fromKey["endpoint"])
	assert.Equal(t, "true", fromKey["insecure_skip_verify"])
	assert.NotContains(t, fromKey, "page_size", "the adapter default applies when the profile sets none")

	fromConnectionString := profile.Settings(config.Secret{ConnectionString: "AccountEndpoint=x;"})
	assert.Empty(t, fromConnectionString["key"])
	assert.Equal(t, "AccountEndpoint=x;", fromConnectionString["connection_string"])
}

func TestSettingsPassThePageSizeThrough(t *testing.T) {
	profile := emulatorProfile()
	profile.PageSize = 25

	assert.Equal(t, "25", profile.Settings(config.Secret{})["page_size"])
}

func TestPutReplacesAnExistingProfile(t *testing.T) {
	changed := emulatorProfile()
	changed.Endpoint = "https://elsewhere:8081"

	cfg, err := twoProfiles(t).Put(changed)
	require.NoError(t, err)

	assert.Equal(t, "https://elsewhere:8081", cfg.Profiles["emulator"].Endpoint)
	assert.Equal(t, []string{"emulator", "prod"}, cfg.Names())
	assert.Equal(t, "emulator", cfg.DefaultProfile)
}
