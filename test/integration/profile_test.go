//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/config"
)

// emptyKeyring has nothing in it, so the key can only come from the
// environment.
type emptyKeyring struct{}

func (emptyKeyring) Get(string) (string, error) { return "", config.ErrSecretNotFound }
func (emptyKeyring) Set(string, string) error   { return nil }
func (emptyKeyring) Delete(string) error        { return config.ErrSecretNotFound }

// TestIntegrationProfileLaunch is the manual checklist of
// docs/plan/06-config-profiles.md: a profile in a real config file, its key
// from the environment, resolves to a connection that reaches the emulator.
func TestIntegrationProfileLaunch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	raw := settings()
	store, err := config.DefaultStore()
	require.NoError(t, err)
	cfg, err := config.Config{}.Add(config.Profile{
		Name:               "emulator",
		Adapter:            cosmos.Name,
		Endpoint:           raw["endpoint"],
		InsecureSkipVerify: true,
	})
	require.NoError(t, err)
	require.NoError(t, store.Save(cfg))
	t.Setenv(config.EnvKeyVar("emulator"), raw["key"])

	loaded, err := store.Load()
	require.NoError(t, err)
	profile, err := loaded.Profile("")
	require.NoError(t, err)
	secret, err := config.SecretResolver{Keyring: emptyKeyring{}}.Resolve(profile.Name)
	require.NoError(t, err)
	assert.Equal(t, "$ALCHEMIST_EMULATOR_KEY", secret.Source)

	conn, err := cosmos.Adapter{}.Connect(context.Background(), profile.Settings(secret))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	waitForEmulator(t, conn)
}
