package cmd_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/cmd"
)

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
