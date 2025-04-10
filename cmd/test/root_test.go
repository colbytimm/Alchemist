package test

import (
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/services"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRoot(t *testing.T) {
	// Create a new ServiceProvider with mocks
	sp := services.NewMockServiceProvider()

	// Call the Root function to get the root command
	rootCmd := cmd.Root(sp)

	// Assert that the root command is properly configured
	assert.Equal(t, "alchemist", rootCmd.Use)
	assert.Contains(t, rootCmd.Long, "A command line tool for querying Cosmos DB in your terminal")
	assert.True(t, rootCmd.CompletionOptions.DisableDefaultCmd)

	// First verify the top-level commands exist
	assert.Equal(t, 2, len(rootCmd.Commands()))
	localCmd := findSubCommand(rootCmd, "local")
	assert.NotNil(t, localCmd, "local command should exist")
	cosmosCmd := findSubCommand(rootCmd, "cosmos")
	assert.NotNil(t, cosmosCmd, "cosmos command should exist")

	// Check that all expected subcommands are present
	localSubcommands := []struct {
		group string
		name  string
		use   string
	}{
		{"account", "add", "account add"},
		{"account", "delete", "account delete"},
		{"account", "list", "account list"},
		{"query", "save", "query save"},
		{"query", "list", "query list"},
		{"query", "delete", "query delete"},
	}

	cosmosSubcommands := []struct {
		group string
		name  string
		use   string
	}{
		{"account", "manage", "account manage"},
		{"data", "query", "data query"},
		{"data", "upload", "data upload"},
	}

	// Verify all expected local subcommands are added
	for _, cmd := range localSubcommands {
		subCmd := findSubCommandByUse(localCmd, cmd.use)
		assert.NotNil(t, subCmd, "Subcommand 'local %s' should exist", cmd.use)
		if subCmd != nil {
			assert.Equal(t, cmd.use, subCmd.Use, "Subcommand 'local %s' should have Use value '%s'", cmd.use, cmd.use)
		}
	}

	// Verify all expected cosmos subcommands are added
	for _, cmd := range cosmosSubcommands {
		subCmd := findSubCommandByUse(cosmosCmd, cmd.use)
		assert.NotNil(t, subCmd, "Subcommand 'cosmos %s' should exist", cmd.use)
		if subCmd != nil {
			assert.Equal(t, cmd.use, subCmd.Use, "Subcommand 'cosmos %s' should have Use value '%s'", cmd.use, cmd.use)
		}
	}

	// Test that non-existent commands return nil
	nonExistentCommands := []struct {
		parent string
		use    string
	}{
		{"local", "fake command"},
		{"local", "account fake"},
		{"local", "query fake"},
		{"cosmos", "fake command"},
		{"cosmos", "data fake"},
	}

	for _, cmd := range nonExistentCommands {
		var parentCmd *cobra.Command
		switch cmd.parent {
		case "local":
			parentCmd = localCmd
		case "cosmos":
			parentCmd = cosmosCmd
		}
		assert.NotNil(t, parentCmd)
		subCmd := findSubCommandByUse(parentCmd, cmd.use)
		assert.Nil(t, subCmd, "Command '%s %s' should not exist", cmd.parent, cmd.use)
	}

	// Test invalid root command
	invalidCmd := findSubCommand(rootCmd, "invalid")
	assert.Nil(t, invalidCmd, "Invalid root command should not exist")
}

// findSubCommand finds a direct subcommand by its name.
func findSubCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

// findSubCommandByUse finds a subcommand by its Use field.
func findSubCommandByUse(parent *cobra.Command, use string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Use == use {
			return cmd
		}
	}
	return nil
}
