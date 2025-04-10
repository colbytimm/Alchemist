package test

import (
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/services"
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

	// Check that all expected subcommands are present
	subcommandNames := []string{
		"add-account",
		"delete-account",
		"list-accounts", // This should be the correct name after our recent change
		"query-account",
		"account-details",
		"batch-upload",
		"save-query",
		"list-query",
		"delete-query",
	}

	// Verify all expected subcommands are added
	for _, name := range subcommandNames {
		subCmd, _, err := rootCmd.Find([]string{name})
		assert.NoError(t, err, "Subcommand %s should exist", name)
		assert.NotNil(t, subCmd, "Subcommand %s should not be nil", name)
	}
}
