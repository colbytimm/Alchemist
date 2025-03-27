package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

func TestAddAccount_Success(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             options.Name,
			ConnectionString: options.ConnectionString,
			Tag:              options.Tag,
			IsDefault:        true,
		}, nil
	}

	// Test the actual function directly, not the command
	accountOptions := &data.AccountOptions{
		Name:             "test-account",
		ConnectionString: "test-connection-string",
		Tag:              "test",
		IsDefault:        true,
	}

	// Get the internal function that handles adding accounts
	result, err := cmd.AddAccountInternal(accountOptions, false)

	// Assertions
	AssertNoError(t, err)
	AssertEqual(t, "test-account", result.Name)
	AssertEqual(t, "test-connection-string", result.ConnectionString)
	AssertEqual(t, "test", result.Tag)
	AssertEqual(t, true, result.IsDefault)
}

func TestAddAccount_DatabaseError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Test the actual function directly
	accountOptions := &data.AccountOptions{
		Name:             "test-account",
		ConnectionString: "test-connection-string",
		Tag:              "test",
		IsDefault:        false,
	}

	// Get the internal function that handles adding accounts
	_, err := cmd.AddAccountInternal(accountOptions, false)

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database error")
}

func TestAddAccount_TableError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return errors.New("table error")
	}

	// Test the actual function directly
	accountOptions := &data.AccountOptions{
		Name:             "test-account",
		ConnectionString: "test-connection-string",
		Tag:              "test",
		IsDefault:        false,
	}

	// Get the internal function that handles adding accounts
	_, err := cmd.AddAccountInternal(accountOptions, false)

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "table error")
}

func TestAddAccount_InsertError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
		return data.AccountOptions{}, errors.New("insert error")
	}

	// Test the actual function directly
	accountOptions := &data.AccountOptions{
		Name:             "test-account",
		ConnectionString: "test-connection-string",
		Tag:              "test",
		IsDefault:        false,
	}

	// Get the internal function that handles adding accounts
	_, err := cmd.AddAccountInternal(accountOptions, false)

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "insert error")
}
