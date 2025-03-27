package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

func TestListAccounts_DatabaseError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Test the internal function directly
	accounts, err := cmd.ListAccountsInternal()

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database error")
	AssertEqual(t, 0, len(accounts)) // Should return nil or empty slice
}

func TestListAccounts_NoAccounts(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{}, nil
	}

	// Test the internal function directly
	accounts, err := cmd.ListAccountsInternal()

	// Assertions
	AssertNoError(t, err)
	AssertEqual(t, 0, len(accounts))
}

func TestListAccounts_WithAccounts(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	// Create test accounts
	testAccounts := []data.AccountOptions{
		{
			Id:               1,
			Name:             "test-account",
			ConnectionString: "connection-string",
			Tag:              "dev",
			IsDefault:        true,
		},
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return testAccounts, nil
	}

	// Test the internal function directly
	accounts, err := cmd.ListAccountsInternal()

	// Assertions
	AssertNoError(t, err)
	AssertEqual(t, 1, len(accounts))
	AssertEqual(t, "test-account", accounts[0].Name)
	AssertEqual(t, "connection-string", accounts[0].ConnectionString)
	AssertEqual(t, "dev", accounts[0].Tag)
	AssertEqual(t, true, accounts[0].IsDefault)
}

func TestListAccounts_GetAccountsError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return nil, errors.New("accounts error")
	}

	// Test the internal function directly
	accounts, err := cmd.ListAccountsInternal()

	// Assertions
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "accounts error")
	AssertEqual(t, 0, len(accounts)) // Should return nil or empty slice
}
