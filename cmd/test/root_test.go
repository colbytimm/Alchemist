package cmd_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
	"github.com/colbytimm/alchemist/internal/adapter/mock"
)

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
	flags := cmd.NewRootCmd().Flags()

	defaults := map[string]string{
		"adapter":              cosmos.Name,
		"endpoint":             "",
		"key":                  "",
		"connection-string":    "",
		"insecure-skip-verify": "false",
		"ascii":                "false",
		"verbose":              "false",
	}
	for name, want := range defaults {
		flag := flags.Lookup(name)
		require.NotNil(t, flag, "--%s should be registered", name)
		assert.Equal(t, want, flag.DefValue, "--%s default", name)
	}
}

func TestUnknownAdapterFails(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--adapter", "postgres"})

	require.ErrorIs(t, root.Execute(), adapter.ErrUnknownAdapter)
}

func TestCosmosWithoutCredentialsNamesTheFlags(t *testing.T) {
	require.NoError(t, ensureAdaptersRegistered())
	root := cmd.NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--adapter", cosmos.Name})

	err := root.Execute()

	require.ErrorIs(t, err, cosmos.ErrMissingCredentials)
	for _, flag := range []string{"--connection-string", "--endpoint", "--key", "--adapter mock"} {
		assert.Contains(t, err.Error(), flag, "the fix should be in the message")
	}
}

func TestEndpointAndKeyAreRequiredTogether(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--endpoint", "https://localhost:8081"})

	require.Error(t, root.Execute())
}

func TestConnectionStringAndEndpointAreMutuallyExclusive(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--connection-string", "x", "--endpoint", "y", "--key", "z"})

	require.Error(t, root.Execute())
}

func TestPositionalArgumentsAreRejected(t *testing.T) {
	root := cmd.NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"query.sql"})

	require.Error(t, root.Execute())
}

// ensureAdaptersRegistered makes the registry state independent of test order.
func ensureAdaptersRegistered() error {
	if err := cmd.RegisterAdapters(); err != nil && !errors.Is(err, adapter.ErrDuplicateName) {
		return err
	}
	return nil
}

func TestNewRootCmd(t *testing.T) {
	root := cmd.NewRootCmd()
	require.NotNil(t, root)
	assert.Equal(t, "alchemist", root.Use)
}

func TestVersionFlag(t *testing.T) {
	root := cmd.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})

	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), app.Name)
	assert.Contains(t, out.String(), app.Version)
	assert.Contains(t, out.String(), app.BuildDate)
}

func TestHelpFlag(t *testing.T) {
	root := cmd.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})

	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), "Cosmos DB")
}
