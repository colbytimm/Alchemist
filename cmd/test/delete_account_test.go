package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/stretchr/testify/assert"
)

func TestDeleteAccount_Success(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.DeleteAccountByNameMock = func(name string) error {
		return nil
	}

	// Test the internal function directly
	err := cmd.DeleteAccountInternal("test-account", mockManager, false)

	// Assertions
	assert.NoError(t, err)
}

func TestDeleteAccount_DatabaseError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Test the internal function directly
	err := cmd.DeleteAccountInternal("test-account", mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestDeleteAccount_DeleteError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.DeleteAccountByNameMock = func(name string) error {
		return errors.New("delete error")
	}

	// Test the internal function directly
	err := cmd.DeleteAccountInternal("test-account", mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete error")
}

func TestDeleteAccount_AccountNotFoundError(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.DeleteAccountByNameMock = func(name string) error {
		return errors.New("account with name 'test-account' not found")
	}

	// Test the internal function directly
	err := cmd.DeleteAccountInternal("test-account", mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account with name 'test-account' not found")
}

func TestDeleteAccount_VerboseOutput(t *testing.T) {
	// Configure the mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.DeleteAccountByNameMock = func(name string) error {
		return nil
	}

	// Test the internal function directly with verbose flag
	err := cmd.DeleteAccountInternal("test-account", mockManager, true)

	// Assertions
	assert.NoError(t, err)
}
