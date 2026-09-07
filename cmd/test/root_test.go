package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
	"github.com/colbytimm/alchemist/internal/config"
)

// fakeKeyring is an in-memory config.Keyring.
type fakeKeyring struct {
	secrets map[string]string
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{secrets: map[string]string{}} }

func (k *fakeKeyring) Get(profile string) (string, error) {
	secret, ok := k.secrets[profile]
	if !ok {
		return "", config.ErrSecretNotFound
	}
	return secret, nil
}

func (k *fakeKeyring) Set(profile, secret string) error {
	k.secrets[profile] = secret
	return nil
}

func (k *fakeKeyring) Delete(profile string) error {
	if _, ok := k.secrets[profile]; !ok {
		return config.ErrSecretNotFound
	}
	delete(k.secrets, profile)
	return nil
}

// harness runs root commands against a private config directory, a private
// state directory, and an in-memory keyring, with nothing inherited from the
// developer's environment.
type harness struct {
	keyring *fakeKeyring
}

func newHarness(t *testing.T) harness {
	t.Helper()
	require.NoError(t, ensureAdaptersRegistered())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("COSMOS_CONNECTION_STRING", "")
	return harness{keyring: newFakeKeyring()}
}

// run executes one fresh root command, feeding input on stdin, and returns
// what it printed.
func (h harness) run(input string, args ...string) (string, error) {
	return h.runContext(context.Background(), input, args...)
}

func (h harness) runContext(ctx context.Context, input string, args ...string) (string, error) {
	root := cmd.NewRootCmd(h.keyring)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

// launch runs the root command the way a launch would, under a context that
// is already cancelled: the TUI returns the moment it starts, which is as far
// as a test without a terminal can follow. tea.ErrProgramKilled therefore
// means the launch reached the TUI.
func (h harness) launch(args ...string) error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.runContext(ctx, "", args...)
	return err
}

// addProfile adds an emulator profile with no key.
func (h harness) addProfile(t *testing.T, name string, extra ...string) {
	t.Helper()
	args := append([]string{"profile", "add", name, "--endpoint", "https://localhost:8081"}, extra...)
	_, err := h.run("", args...)
	require.NoError(t, err)
}

func configPath(t *testing.T) string {
	t.Helper()
	store, err := config.DefaultStore()
	require.NoError(t, err)
	return store.Path
}

// ensureAdaptersRegistered makes the registry state independent of test order.
func ensureAdaptersRegistered() error {
	if err := cmd.RegisterAdapters(); err != nil && !errors.Is(err, adapter.ErrDuplicateName) {
		return err
	}
	return nil
}

func TestRegisterAdapters(t *testing.T) {
	// The registry is process-wide, so under -count>1 it is already there.
	if err := cmd.RegisterAdapters(); err != nil {
		require.ErrorIs(t, err, adapter.ErrDuplicateName)
	}

	for _, name := range []string{cosmos.Name, mock.Name} {
		factory, err := adapter.Get(name)
		require.NoError(t, err)
		assert.Equal(t, name, factory().Name())
	}

	// A repeat call is a wiring bug and must surface rather than be swallowed.
	require.ErrorIs(t, cmd.RegisterAdapters(), adapter.ErrDuplicateName)
}

func TestSessionFlags(t *testing.T) {
	flags := cmd.NewRootCmd(newFakeKeyring()).Flags()

	defaults := map[string]string{
		"adapter": "",
		"ascii":   "false",
		"verbose": "false",
	}
	for name, want := range defaults {
		flag := flags.Lookup(name)
		require.NotNil(t, flag, "--%s should be registered", name)
		assert.Equal(t, want, flag.DefValue, "--%s default", name)
	}
	for _, retired := range []string{"endpoint", "key", "connection-string", "insecure-skip-verify"} {
		assert.Nil(t, flags.Lookup(retired), "--%s belongs to profiles now", retired)
	}
}

func TestUnknownAdapterFails(t *testing.T) {
	_, err := newHarness(t).run("", "--adapter", "postgres")

	require.ErrorIs(t, err, adapter.ErrUnknownAdapter)
}

func TestCosmosWithoutAProfileSaysHowToGetOne(t *testing.T) {
	_, err := newHarness(t).run("", "--adapter", cosmos.Name)

	require.ErrorIs(t, err, cosmos.ErrMissingCredentials)
	assert.Contains(t, err.Error(), "run alchemist")
	assert.Contains(t, err.Error(), "--adapter mock")
}

func TestMockAdapterLaunchesWithoutAProfile(t *testing.T) {
	err := newHarness(t).launch("--adapter", mock.Name)

	require.ErrorIs(t, err, tea.ErrProgramKilled)
}

func TestNoProfilesOpensTheConnectScreen(t *testing.T) {
	err := newHarness(t).launch()

	require.ErrorIs(t, err, tea.ErrProgramKilled)
}

func TestMissingKeyOpensTheConnectScreen(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	err := h.launch("emulator")

	require.ErrorIs(t, err, tea.ErrProgramKilled)
	assert.Empty(t, h.keyring.secrets, "nothing is stored until the screen connects")
}

func TestResolvedProfileLaunchesStraightIn(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "dev", "--adapter", mock.Name)
	t.Setenv(config.EnvKeyVar("dev"), "from-env")

	err := h.launch("dev")

	require.ErrorIs(t, err, tea.ErrProgramKilled)
}

func TestUnknownProfileFails(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "staging")

	require.ErrorIs(t, err, config.ErrProfileNotFound)
}

func TestProfileAndAdapterAreAlternatives(t *testing.T) {
	_, err := newHarness(t).run("", "emulator", "--adapter", mock.Name)

	require.Error(t, err)
}

func TestTwoPositionalArgumentsAreRejected(t *testing.T) {
	_, err := newHarness(t).run("", "emulator", "prod")

	require.Error(t, err)
}

func TestNewRootCmd(t *testing.T) {
	root := cmd.NewRootCmd(newFakeKeyring())
	require.NotNil(t, root)
	assert.Equal(t, "alchemist [profile]", root.Use)
}

func TestVersionFlag(t *testing.T) {
	out, err := newHarness(t).run("", "--version")

	require.NoError(t, err)
	assert.Contains(t, out, app.Name)
	assert.Contains(t, out, app.Version)
	assert.Contains(t, out, app.BuildDate)
}

func TestHelpFlag(t *testing.T) {
	out, err := newHarness(t).run("", "--help")

	require.NoError(t, err)
	assert.Contains(t, out, "Cosmos DB")
	assert.Contains(t, out, "profile", "the help must point at the profile subcommand")
}

func TestNoLogDirectoryIsCreatedWhenTheSessionNeverStarts(t *testing.T) {
	h := newHarness(t)
	h.addProfile(t, "emulator")

	_, err := h.run("", "staging")
	require.Error(t, err)

	_, statErr := os.Stat(os.Getenv("XDG_STATE_HOME") + "/alchemist")
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
