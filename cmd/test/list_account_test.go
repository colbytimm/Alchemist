package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
)

func TestListAccounts_DatabaseError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Test the internal function directly
	accounts, err := cmd.ListAccountInternal(mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.Equal(t, 0, len(accounts)) // Should return nil or empty slice
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
	accounts, err := cmd.ListAccountInternal(mockManager, false)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, 0, len(accounts))
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
	accounts, err := cmd.ListAccountInternal(mockManager, false)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, 1, len(accounts))
	assert.Equal(t, "test-account", accounts[0].Name)
	assert.Equal(t, "connection-string", accounts[0].ConnectionString)
	assert.Equal(t, "dev", accounts[0].Tag)
	assert.Equal(t, true, accounts[0].IsDefault)
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
	accounts, err := cmd.ListAccountInternal(mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "accounts error")
	assert.Equal(t, 0, len(accounts)) // Should return nil or empty slice
}
