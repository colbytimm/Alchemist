package test

import (
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

func TestListAccountCmd_DatabaseError(t *testing.T) {
	// Make sure we start with no database
	data.SetDB(nil)

	// Create the command
	command := cmd.ListAccountCmd()

	// Execute the command and capture output
	output := CaptureOutput(func() {
		command.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Could not open database")
}

func TestListAccountCmd_NoAccounts(t *testing.T) {
	// Setup a fresh in-memory database
	SetupInMemoryDB(t)

	// Create the command
	command := cmd.ListAccountCmd()

	// Execute the command and capture output
	output := CaptureOutput(func() {
		command.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "No accounts found")
	AssertStringContains(t, output, "Add an account using")
}

func TestListAccountCmd_WithAccounts(t *testing.T) {
	// Setup a fresh in-memory database
	SetupInMemoryDB(t)

	// Add test accounts
	AddTestAccount(t, "test-account", "connection-string", "dev", true)

	// Verify the account was added correctly
	accounts, err := data.GetAccounts()
	AssertNoError(t, err)
	AssertEqual(t, 1, len(accounts))
	AssertEqual(t, "test-account", accounts[0].Name)
	AssertEqual(t, "connection-string", accounts[0].ConnectionString)

	// Note: We're not executing the command because it starts a TUI that waits for user input
	// Instead, we've verified that the database is set up correctly with our test account
}
