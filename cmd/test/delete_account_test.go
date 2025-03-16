package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

func TestDeleteAccountCmd_Success(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             name,
			ConnectionString: "test-connection-string",
			Tag:              "test",
			IsDefault:        false,
		}, nil
	}

	data.DeleteAccountByNameMock = func(name string) error {
		return nil
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags
	cmd.SetArgs([]string{
		"--name", "test-account",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Account deleted successfully")
	AssertStringContains(t, output, "test-account")
}

func TestDeleteAccountCmd_DatabaseError(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags
	cmd.SetArgs([]string{
		"--name", "test-account",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Could not open database")
	AssertStringContains(t, output, "database error")
}

func TestDeleteAccountCmd_TableError(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.DeleteAccountByNameMock = func(name string) error {
		return errors.New("table error")
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags
	cmd.SetArgs([]string{
		"--name", "test-account",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Failed to delete account")
	AssertStringContains(t, output, "table error")
}

func TestDeleteAccountCmd_AccountNotFoundError(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.DeleteAccountByNameMock = func(name string) error {
		return errors.New("account with name 'test-account' not found")
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags
	cmd.SetArgs([]string{
		"--name", "test-account",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Failed to delete account")
	AssertStringContains(t, output, "account with name 'test-account' not found")
}

func TestDeleteAccountCmd_DeleteError(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             name,
			ConnectionString: "test-connection-string",
			Tag:              "test",
			IsDefault:        false,
		}, nil
	}

	data.DeleteAccountByNameMock = func(name string) error {
		return errors.New("delete error")
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags
	cmd.SetArgs([]string{
		"--name", "test-account",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Failed to delete account")
	AssertStringContains(t, output, "delete error")
}

func TestDeleteAccountCmd_MissingRequiredFlags(t *testing.T) {
	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// No flags set - missing required name flag

	// Prepare to capture the error
	var err error

	// Execute the command - it should fail because of missing required flags
	output := CaptureOutput(func() {
		err = cmd.Execute()
	})

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, output, "required flag")
}

func TestDeleteAccountCmd_VerboseOutput(t *testing.T) {
	// Setup
	data.ActivateMock()
	defer data.DeactivateMock()

	// Set up mock functions
	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             name,
			ConnectionString: "test-connection-string",
			Tag:              "test",
			IsDefault:        false,
		}, nil
	}

	data.DeleteAccountByNameMock = func(name string) error {
		return nil
	}

	// Create the command
	cmd := cmd.DeleteAccountCmd()

	// Set required flags including verbose
	cmd.SetArgs([]string{
		"--name", "test-account",
		"--verbose",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		cmd.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "Account deleted successfully")
	AssertStringContains(t, output, "test-account")
	AssertStringContains(t, output, "Debug logging enabled")
}
