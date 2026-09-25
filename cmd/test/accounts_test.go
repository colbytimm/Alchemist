package cmd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

func (h harness) profiles(t *testing.T) cmd.Profiles {
	t.Helper()
	store, err := config.DefaultStore()
	require.NoError(t, err)
	return cmd.Profiles{Store: store, Keyring: h.keyring}
}

func TestAccountsListsEveryProfileInOrder(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "staging")
	h.addProfile(t, "emulator", "--insecure-skip-verify")

	accounts, err := h.profiles(t).Accounts()

	require.NoError(t, err)
	assert.Equal(t, []tui.Account{
		{Name: "emulator", Endpoint: "https://localhost:8081", SkipVerify: true, SampleFields: true},
		{Name: "staging", Endpoint: "https://localhost:8081", SampleFields: true},
	}, accounts)
}

func TestOpenAsksForCredentialsWhenNoKeyResolves(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)

	_, err := h.profiles(t).Open(context.Background(), "dev")

	require.ErrorIs(t, err, tui.ErrCredentialsNeeded)
}

func TestOpenSeesAProfileAddedAfterItWasBuilt(t *testing.T) {
	h := newHarness(t)
	profiles := h.profiles(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	t.Setenv(config.EnvKeyVar("dev"), "from-env")

	conn, err := profiles.Open(context.Background(), "dev")

	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func TestOpenRefusesAnUnknownProfile(t *testing.T) {
	_, err := newHarness(t).profiles(t).Open(context.Background(), "nowhere")

	require.ErrorIs(t, err, config.ErrNoProfiles)
}

func TestConnectRefusesAProfileThatAlreadyWorks(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	h.keyring.secrets["dev"] = "stored-key"

	_, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: "dev", Endpoint: "https://elsewhere", Key: "typed-key",
	})

	require.ErrorIs(t, err, config.ErrProfileExists)
	assert.Equal(t, "stored-key", h.keyring.secrets["dev"], "the working profile is left alone")
}

func TestConnectCompletesAProfileWithNoKey(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)

	conn, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: "dev", Endpoint: "https://elsewhere", Key: "typed-key", StoreKey: true,
	})

	require.NoError(t, err)
	require.NoError(t, conn.Close())
	assert.Equal(t, "typed-key", h.keyring.secrets["dev"])
	accounts, err := h.profiles(t).Accounts()
	require.NoError(t, err)
	assert.Equal(t, []tui.Account{{Name: "dev", Endpoint: "https://elsewhere", SampleFields: true, ReadOnly: true}}, accounts)
}

func TestConnectAddsAnyOtherNameOnlyOnceItConnects(t *testing.T) {
	h := newHarness(t)

	_, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: "fresh", Endpoint: "::not-a-url", Key: "a2V5", StoreKey: true,
	})

	require.Error(t, err, "the new profile reaches the adapter, which refuses the endpoint")
	assert.NotErrorIs(t, err, config.ErrProfileExists)
	accounts, loadErr := h.profiles(t).Accounts()
	require.NoError(t, loadErr)
	assert.Empty(t, accounts, "an attempt that did not connect leaves nothing behind")
	assert.Empty(t, h.keyring.secrets)
}

// lockedKeyring cannot say whether it holds a key.
type lockedKeyring struct{ *fakeKeyring }

func (lockedKeyring) Get(string) (string, error) { return "", errors.New("keychain is locked") }

func TestConnectCompletesAProfileWhoseKeychainCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	store, err := config.DefaultStore()
	require.NoError(t, err)
	profiles := cmd.Profiles{Store: store, Keyring: lockedKeyring{h.keyring}}

	conn, err := profiles.Connect(context.Background(), panes.ConnectForm{
		Profile: "dev", Endpoint: "https://elsewhere", Key: "typed-key",
	})

	require.NoError(t, err, "a machine with no keychain can still connect a keyless profile")
	require.NoError(t, conn.Close())
}

func TestOpenAsksForTheKeyWhenTheKeychainCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	store, err := config.DefaultStore()
	require.NoError(t, err)

	_, err = cmd.Profiles{Store: store, Keyring: lockedKeyring{h.keyring}}.Open(context.Background(), "dev")

	require.ErrorIs(t, err, tui.ErrCredentialsNeeded)
}

// refusingKeyring will not store a key.
type refusingKeyring struct{ *fakeKeyring }

func (refusingKeyring) Set(string, string) error { return errors.New("keychain refused the write") }

func TestConnectSavesNothingWhenTheKeyCannotBeRemembered(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	store, err := config.DefaultStore()
	require.NoError(t, err)
	profiles := cmd.Profiles{Store: store, Keyring: refusingKeyring{h.keyring}}

	_, err = profiles.Connect(context.Background(), panes.ConnectForm{
		Profile: "dev", Endpoint: "https://elsewhere", Key: "typed-key", StoreKey: true,
	})

	require.ErrorContains(t, err, "keychain refused the write")
	accounts, loadErr := profiles.Accounts()
	require.NoError(t, loadErr)
	assert.Equal(t, "https://localhost:8081", accounts[0].Endpoint,
		"the profile is untouched, so a retry without remembering the key starts clean")
}

func TestConnectRefusesACaseVariantBeforeConnecting(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "prod", "--adapter", mock.Name)

	_, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: "Prod", Endpoint: "::not-a-url", Key: "typed-key",
	})

	require.ErrorIs(t, err, config.ErrProfileExists, "refused by name, before the endpoint is even tried")
}

// A connection string for staging must never connect prod: it names its own
// account, which is not the one the switcher and status bar would show.
func TestOpenNeverBorrowsAnotherAccountsConnectionString(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "prod", "--adapter", mock.Name, "--endpoint", "https://prod.documents.azure.com:443/")
	h.addProfile(t, "staging", "--adapter", mock.Name, "--endpoint", "https://staging.documents.azure.com:443/")
	t.Setenv("COSMOS_CONNECTION_STRING", "AccountEndpoint=https://staging.documents.azure.com:443/;AccountKey=a2V5;")

	_, err := h.profiles(t).Open(context.Background(), "prod")
	require.ErrorIs(t, err, tui.ErrCredentialsNeeded, "prod asks for its own key")

	conn, err := h.profiles(t).Open(context.Background(), "staging")
	require.NoError(t, err, "the account the string names still connects with it")
	require.NoError(t, conn.Close())
}
