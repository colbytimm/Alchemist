package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestDeleteQueryCmd_DatabaseError(t *testing.T) {
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
	cmd := cmd.DeleteQueryCmd(sp)

	// Execute the command - need to pass the required flag
	cmd.SetArgs([]string{"--name", "test-query"})
	err := cmd.Execute()

	// Command should execute without error even if there are internal errors
	assert.NoError(t, err)
}

func TestDeleteQueryCmd_EnsureTableError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalEnsureTable := mockManager.EnsureSavedQueryTableExistsMock
	defer func() {
		mockManager.EnsureSavedQueryTableExistsMock = originalEnsureTable
	}()

	// Set up mock for table creation error
	mockManager.EnsureSavedQueryTableExistsMock = func() error {
		return errors.New("table error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.DeleteQueryCmd(sp)

	// Execute the command - need to pass the required flag
	cmd.SetArgs([]string{"--name", "test-query"})
	err := cmd.Execute()

	// Command should execute without error even if there are internal errors
	assert.NoError(t, err)
}

func TestDeleteQueryCmd_QueryNotFound(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueryByName := mockManager.GetSavedQueryByNameMock
	defer func() {
		mockManager.GetSavedQueryByNameMock = originalGetQueryByName
	}()

	// Set up mock for query not found error
	mockManager.GetSavedQueryByNameMock = func(name string) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{}, errors.New("query not found")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.DeleteQueryCmd(sp)

	// Execute the command - need to pass the required flag
	cmd.SetArgs([]string{"--name", "non-existent-query"})
	err := cmd.Execute()

	// Command should execute without error even if there are internal errors
	assert.NoError(t, err)
}

func TestDeleteQueryCmd_DeleteError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueryByName := mockManager.GetSavedQueryByNameMock
	originalDeleteByName := mockManager.DeleteSavedQueryByNameMock
	defer func() {
		mockManager.GetSavedQueryByNameMock = originalGetQueryByName
		mockManager.DeleteSavedQueryByNameMock = originalDeleteByName
	}()

	// Set up mock for successful query find but deletion error
	mockManager.GetSavedQueryByNameMock = func(name string) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{
			Name: name,
		}, nil
	}

	mockManager.DeleteSavedQueryByNameMock = func(name string) error {
		return errors.New("delete error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.DeleteQueryCmd(sp)

	// Execute the command - need to pass the required flag
	cmd.SetArgs([]string{"--name", "test-query"})
	err := cmd.Execute()

	// Command should execute without error even if there are internal errors
	assert.NoError(t, err)
}

func TestDeleteQueryCmd_Success(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueryByName := mockManager.GetSavedQueryByNameMock
	originalDeleteByName := mockManager.DeleteSavedQueryByNameMock
	defer func() {
		mockManager.GetSavedQueryByNameMock = originalGetQueryByName
		mockManager.DeleteSavedQueryByNameMock = originalDeleteByName
	}()

	// Set up mock for successful query find and deletion
	mockManager.GetSavedQueryByNameMock = func(name string) (data.SavedQueryOptions, error) {
		return data.SavedQueryOptions{
			Name: name,
		}, nil
	}

	mockManager.DeleteSavedQueryByNameMock = func(name string) error {
		return nil
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.DeleteQueryCmd(sp)

	// Execute the command - need to pass the required flag
	cmd.SetArgs([]string{"--name", "test-query"})
	err := cmd.Execute()

	// Command should execute without error
	assert.NoError(t, err)
}

func TestDeleteQueryCmd_MissingName(t *testing.T) {
	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.DeleteQueryCmd(sp)

	// Save the original Run function to avoid actual execution
	originalRun := cmd.Run
	defer func() {
		cmd.Run = originalRun
	}()

	// Replace Run with a no-op function to test flag validation only
	cmd.Run = func(cmd *cobra.Command, args []string) {}

	// Execute the command without the required name flag
	cmd.SetArgs([]string{})
	err := cmd.Execute()

	// Command should fail because name is required
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"name\" not set")
}
