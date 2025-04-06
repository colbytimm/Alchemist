package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/stretchr/testify/assert"
)

func TestSaveQueryCmd_MissingRequiredFlags(t *testing.T) {
	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Test with missing name flag
	cmdInstance := cmd.SaveQueryCmd(sp)
	cmdInstance.SetArgs([]string{"--query", "SELECT * FROM db.container as c"})
	err := cmdInstance.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"name\" not set")

	// Test with missing query flag
	cmdInstance = cmd.SaveQueryCmd(sp)
	cmdInstance.SetArgs([]string{"--name", "test-query"})
	err = cmdInstance.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"query\" not set")
}

func TestSaveQueryCmd_InvalidQuery(t *testing.T) {
	// Save original function and restore it after test
	originalValidateQuery := cmd.ValidateQueryFunc
	defer func() {
		cmd.ValidateQueryFunc = originalValidateQuery
	}()

	// Mock the ValidateQuery function to return an error
	cmd.ValidateQueryFunc = func(query string) error {
		return errors.New("invalid query format")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command with valid name but invalid query
	cmdInstance.SetArgs([]string{"--name", "test-query", "--query", "INVALID QUERY"})
	err := cmdInstance.Execute()

	// Command should execute without error but internal error handling should catch the validation error
	assert.NoError(t, err)
}

func TestSaveQueryCmd_DatabaseError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalOpenDatabase := mockManager.OpenDatabaseMock
	defer func() {
		mockManager.OpenDatabaseMock = originalOpenDatabase
	}()

	// Set up mock for database error
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command with valid name and query
	cmdInstance.SetArgs([]string{"--name", "test-query", "--query", "SELECT * FROM test-db.test-container as c"})
	err := cmdInstance.Execute()

	// Command should execute without error even if there are internal database errors
	assert.NoError(t, err)
}

func TestSaveQueryCmd_TableError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalEnsureTable := mockManager.EnsureSavedQueryTableExistsMock
	defer func() {
		mockManager.EnsureSavedQueryTableExistsMock = originalEnsureTable
	}()

	// Set up mock for table error
	mockManager.EnsureSavedQueryTableExistsMock = func() error {
		return errors.New("table error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command
	cmdInstance.SetArgs([]string{"--name", "test-query", "--query", "SELECT * FROM test-db.test-container as c"})
	err := cmdInstance.Execute()

	// Command should execute without error even if there are internal table errors
	assert.NoError(t, err)
}

func TestSaveQueryCmd_SaveError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalSaveQuery := mockManager.SaveQueryMock
	defer func() {
		mockManager.SaveQueryMock = originalSaveQuery
	}()

	// Set up mock for save error
	mockManager.SaveQueryMock = func(options *data.SavedQueryOptions) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{}, errors.New("save error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command
	cmdInstance.SetArgs([]string{"--name", "test-query", "--query", "SELECT * FROM test-db.test-container as c"})
	err := cmdInstance.Execute()

	// Command should execute without error even if there are internal save errors
	assert.NoError(t, err)
}

func TestSaveQueryCmd_Success(t *testing.T) {
	// Save original function and restore it after test
	originalValidateQuery := cmd.ValidateQueryFunc
	defer func() {
		cmd.ValidateQueryFunc = originalValidateQuery
	}()

	// Mock the ValidateQuery function to return no error
	cmd.ValidateQueryFunc = func(query string) error {
		return nil
	}

	// Save the original mock functions to restore after test
	originalSaveQuery := mockManager.SaveQueryMock
	defer func() {
		mockManager.SaveQueryMock = originalSaveQuery
	}()

	// Set up mock for successful save
	mockManager.SaveQueryMock = func(options *data.SavedQueryOptions) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{
			Id:           1,
			Name:         options.Name,
			QueryString:  options.QueryString,
			DatabaseId:   "test-db",
			ContainerId:  "test-container",
			AccountName:  options.AccountName,
			Description:  options.Description,
			DateCreated:  "2023-01-01",
			DateModified: "2023-01-01",
		}, nil
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command with all possible flags
	cmdInstance.SetArgs([]string{
		"--name", "test-query",
		"--query", "SELECT * FROM test-db.test-container as c",
		"--account", "test-account",
		"--description", "Test query description",
		"--verbose",
	})
	err := cmdInstance.Execute()

	// Command should execute without error
	assert.NoError(t, err)
}

func TestSaveQueryCmd_SuccessWithoutOptionalFlags(t *testing.T) {
	// Save original function and restore it after test
	originalValidateQuery := cmd.ValidateQueryFunc
	defer func() {
		cmd.ValidateQueryFunc = originalValidateQuery
	}()

	// Mock the ValidateQuery function to return no error
	cmd.ValidateQueryFunc = func(query string) error {
		return nil
	}

	// Save the original mock functions to restore after test
	originalSaveQuery := mockManager.SaveQueryMock
	defer func() {
		mockManager.SaveQueryMock = originalSaveQuery
	}()

	// Set up mock for successful save
	mockManager.SaveQueryMock = func(options *data.SavedQueryOptions) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{
			Id:           1,
			Name:         options.Name,
			QueryString:  options.QueryString,
			DatabaseId:   "test-db",
			ContainerId:  "test-container",
			AccountName:  "",
			Description:  "",
			DateCreated:  "2023-01-01",
			DateModified: "2023-01-01",
		}, nil
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmdInstance := cmd.SaveQueryCmd(sp)

	// Execute the command with only required flags
	cmdInstance.SetArgs([]string{
		"--name", "test-query",
		"--query", "SELECT * FROM test-db.test-container as c",
	})
	err := cmdInstance.Execute()

	// Command should execute without error
	assert.NoError(t, err)
}
