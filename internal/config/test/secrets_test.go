package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/config"
)

// fakeKeyring is an in-memory Keyring. A non-nil err stands in for a
// keychain that cannot be reached at all.
type fakeKeyring struct {
	secrets map[string]string
	err     error
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{secrets: map[string]string{}} }

func (k *fakeKeyring) Get(profile string) (string, error) {
	if k.err != nil {
		return "", k.err
	}
	secret, ok := k.secrets[profile]
	if !ok {
		return "", config.ErrSecretNotFound
	}
	return secret, nil
}

func (k *fakeKeyring) Set(profile, secret string) error {
	if k.err != nil {
		return k.err
	}
	k.secrets[profile] = secret
	return nil
}

func (k *fakeKeyring) Delete(profile string) error {
	if k.err != nil {
		return k.err
	}
	if _, ok := k.secrets[profile]; !ok {
		return config.ErrSecretNotFound
	}
	delete(k.secrets, profile)
	return nil
}

var errNoKeychain = errors.New("dbus: no secret service")

func TestEnvKeyVar(t *testing.T) {
	tests := []struct{ profile, want string }{
		{"emulator", "ALCHEMIST_EMULATOR_KEY"},
		{"my-emulator", "ALCHEMIST_MY_EMULATOR_KEY"},
		{"Prod_2", "ALCHEMIST_PROD_2_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			assert.Equal(t, tt.want, config.EnvKeyVar(tt.profile))
		})
	}
}

func TestResolveOrder(t *testing.T) {
	tests := []struct {
		name    string
		keyring *fakeKeyring
		env     map[string]string
		want    config.Secret
		wantErr error
	}{
		{
			name:    "keychain beats the environment",
			keyring: &fakeKeyring{secrets: map[string]string{"prod": "from-keychain"}},
			env:     map[string]string{"ALCHEMIST_PROD_KEY": "from-env"},
			want:    config.Secret{Key: "from-keychain", Source: config.SourceKeychain},
		},
		{
			name:    "environment when the keychain has no entry",
			keyring: newFakeKeyring(),
			env:     map[string]string{"ALCHEMIST_PROD_KEY": "from-env"},
			want:    config.Secret{Key: "from-env", Source: "$ALCHEMIST_PROD_KEY"},
		},
		{
			name:    "environment when the keychain is unavailable",
			keyring: &fakeKeyring{err: errNoKeychain},
			env:     map[string]string{"ALCHEMIST_PROD_KEY": "from-env"},
			want:    config.Secret{Key: "from-env", Source: "$ALCHEMIST_PROD_KEY"},
		},
		{
			name:    "connection string as the last resort",
			keyring: newFakeKeyring(),
			env:     map[string]string{"COSMOS_CONNECTION_STRING": prodConnectionString},
			want:    config.Secret{ConnectionString: prodConnectionString, Source: "$COSMOS_CONNECTION_STRING"},
		},
		{
			name:    "nothing anywhere",
			keyring: newFakeKeyring(),
			wantErr: config.ErrSecretNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALCHEMIST_PROD_KEY", "")
			t.Setenv("COSMOS_CONNECTION_STRING", "")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			got, err := config.SecretResolver{Keyring: tt.keyring}.Resolve(prodProfile())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveNamesEveryPlaceItLooked(t *testing.T) {
	t.Setenv("ALCHEMIST_MY_EMULATOR_KEY", "")
	t.Setenv("COSMOS_CONNECTION_STRING", "")

	_, err := config.SecretResolver{Keyring: newFakeKeyring()}.Resolve(config.Profile{Name: "my-emulator", Endpoint: "https://localhost:8081"})

	require.ErrorIs(t, err, config.ErrSecretNotFound)
	assert.Contains(t, err.Error(), "keychain")
	assert.Contains(t, err.Error(), "$ALCHEMIST_MY_EMULATOR_KEY")
	assert.Contains(t, err.Error(), "$COSMOS_CONNECTION_STRING")
}

func TestResolveKeepsAnUnreachableKeychainDiagnosable(t *testing.T) {
	t.Setenv("ALCHEMIST_PROD_KEY", "")
	t.Setenv("COSMOS_CONNECTION_STRING", "")

	_, err := config.SecretResolver{Keyring: &fakeKeyring{err: errNoKeychain}}.Resolve(prodProfile())

	require.ErrorIs(t, err, config.ErrSecretNotFound)
	assert.ErrorIs(t, err, errNoKeychain)
}

func TestWellKnownKeyComesFirst(t *testing.T) {
	t.Setenv("ALCHEMIST_EMULATOR_KEY", "from-env")
	profile := config.Profile{Name: "emulator", Adapter: "cosmos", Endpoint: "http://localhost:8081", WellKnownKey: true}
	keyring := &fakeKeyring{secrets: map[string]string{"emulator": "from-keychain"}}

	got, err := config.SecretResolver{Keyring: keyring}.Resolve(profile)

	require.NoError(t, err)
	assert.Equal(t, config.Secret{Key: config.EmulatorKey, Source: config.SourceWellKnown}, got)
}

const prodConnectionString = "AccountEndpoint=https://myaccount.documents.azure.com:443/;AccountKey=y;"

func TestAConnectionStringServesOnlyTheAccountItNames(t *testing.T) {
	tests := []struct {
		name             string
		connectionString string
		want             bool
	}{
		{name: "same endpoint", connectionString: prodConnectionString, want: true},
		{name: "default port left off", connectionString: "AccountEndpoint=https://myaccount.documents.azure.com/;AccountKey=y;", want: true},
		{name: "case and spacing", connectionString: " accountEndpoint=HTTPS://MyAccount.documents.azure.com:443 ; AccountKey=y", want: true},
		{name: "another account", connectionString: "AccountEndpoint=https://staging.documents.azure.com:443/;AccountKey=y;"},
		{name: "another port", connectionString: "AccountEndpoint=https://myaccount.documents.azure.com:8081/;AccountKey=y;"},
		{name: "another scheme", connectionString: "AccountEndpoint=http://myaccount.documents.azure.com:443/;AccountKey=y;"},
		{name: "no endpoint", connectionString: "AccountKey=y;"},
		{name: "malformed endpoint", connectionString: "AccountEndpoint=::;AccountKey=y;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALCHEMIST_PROD_KEY", "")
			t.Setenv("COSMOS_CONNECTION_STRING", tt.connectionString)

			secret, err := config.SecretResolver{Keyring: newFakeKeyring()}.Resolve(prodProfile())

			if !tt.want {
				require.ErrorIs(t, err, config.ErrSecretNotFound)
				assert.Contains(t, err.Error(), "not for this account")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.connectionString, secret.ConnectionString)
		})
	}
}
