package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
)

func TestLocalAccountAdd_Success(t *testing.T) {
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
	result, err := cmd.AddAccountInternal(accountOptions, mockManager, false)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, "test-account", result.Name)
	assert.Equal(t, "test-connection-string", result.ConnectionString)
	assert.Equal(t, "test", result.Tag)
	assert.Equal(t, true, result.IsDefault)
}

func TestLocalAccountAdd_DatabaseError(t *testing.T) {
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
	_, err := cmd.AddAccountInternal(accountOptions, mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestLocalAccountAdd_TableError(t *testing.T) {
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
	_, err := cmd.AddAccountInternal(accountOptions, mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table error")
}

func TestLocalAccountAdd_InsertError(t *testing.T) {
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
	_, err := cmd.AddAccountInternal(accountOptions, mockManager, false)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insert error")
}
