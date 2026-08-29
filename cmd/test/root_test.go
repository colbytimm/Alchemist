package cmd_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

func TestRegisterAdapters(t *testing.T) {
	// The registry is process-wide, so under -count>1 it is already there.
	if err := cmd.RegisterAdapters(); err != nil {
		require.ErrorIs(t, err, adapter.ErrDuplicateName)
	}

	factory, err := adapter.Get(cosmos.Name)
	require.NoError(t, err)
	assert.Equal(t, cosmos.Name, factory().Name())

	// A repeat call is a wiring bug and must surface rather than be swallowed.
	require.ErrorIs(t, cmd.RegisterAdapters(), adapter.ErrDuplicateName)
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
