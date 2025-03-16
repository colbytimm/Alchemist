package test

import (
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

func TestAccountDetailsCmd_DatabaseError(t *testing.T) {
	// Make sure we start with no database
	data.SetDB(nil)

	// Create the command
	command := cmd.AccountDetailsCmd()

	// Execute the command and capture output
	output := CaptureOutput(func() {
		command.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Error opening database")
}

func TestAccountDetailsCmd_NoAccounts(t *testing.T) {
	// Setup a fresh in-memory database
	SetupInMemoryDB(t)

	// Create the command
	command := cmd.AccountDetailsCmd()

	// Execute the command and capture output
	output := CaptureOutput(func() {
		command.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "No accounts found")
	AssertStringContains(t, output, "Add an account using")
}
