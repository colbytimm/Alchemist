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

	_, _, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: "dev", Endpoint: "https://elsewhere", Key: "typed-key",
	})

	require.ErrorIs(t, err, config.ErrProfileExists)
	assert.Equal(t, "stored-key", h.keyring.secrets["dev"], "the working profile is left alone")
}

func TestConnectCompletesAProfileWithNoKey(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)

	_, conn, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
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

	_, _, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
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

	_, conn, err := profiles.Connect(context.Background(), panes.ConnectForm{
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

	_, _, err = profiles.Connect(context.Background(), panes.ConnectForm{
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

	_, _, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
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

// connectForm completes the keyless mock profile name through the form,
// with endpoint as typed.
func connectForm(t *testing.T, h harness, name, endpoint string) tui.Account {
	t.Helper()
	account, conn, err := h.profiles(t).Connect(context.Background(), panes.ConnectForm{
		Profile: name, Endpoint: endpoint, Key: "typed-key",
	})
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	return account
}

func TestConnectReportsTheAccountAsSaved(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name, "--database", "sales")

	account := connectForm(t, h, "dev", "https://localhost:8081")

	accounts, err := h.profiles(t).Accounts()
	require.NoError(t, err)
	assert.Equal(t, accounts, []tui.Account{account})
	assert.Equal(t, "sales", account.Database)
}

func TestAFormKeepsAnExplicitReadOnlyOnTheSameEndpoint(t *testing.T) {
	h := newHarness(t)
	_, err := h.run("", "profile", "add", "staging", "--adapter", mock.Name,
		"--endpoint", "https://staging.documents.azure.com:443/", "--read-only=false")
	require.NoError(t, err)

	account := connectForm(t, h, "staging", "https://staging.documents.azure.com:443/")

	assert.False(t, account.ReadOnly, "read_only = false still holds")
	assert.False(t, readOnlyOf(t, h, "staging"))
}

func TestAFormThatChangesTheEndpointDerivesReadOnlyAgain(t *testing.T) {
	h := newHarness(t)
	_, err := h.run("", "profile", "add", "staging", "--adapter", mock.Name,
		"--endpoint", "https://staging.documents.azure.com:443/", "--read-only=false")
	require.NoError(t, err)

	account := connectForm(t, h, "staging", "https://prod.documents.azure.com:443/")

	assert.True(t, account.ReadOnly, "writes were allowed for the old endpoint, not this one")
	assert.True(t, readOnlyOf(t, h, "staging"), "and the saved profile says so too")
}

func TestAFormFromLocalhostToARemoteEndpointIsReadOnly(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)

	account := connectForm(t, h, "dev", "https://prod.documents.azure.com:443/")

	assert.True(t, account.ReadOnly)
}
