package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/stretchr/testify/assert"
)

func TestListQueriesInternal_DatabaseError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalOpenDatabase := mockManager.OpenDatabaseMock
	defer func() {
		mockManager.OpenDatabaseMock = originalOpenDatabase
	}()

	// Set up mock for database error
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	// Call the function and check the result
	queries, err := cmd.ListQueriesInternal(mockManager)

	// Assert expectations
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.Nil(t, queries)
}

func TestListQueriesInternal_TableError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalEnsureTable := mockManager.EnsureSavedQueryTableExistsMock
	defer func() {
		mockManager.EnsureSavedQueryTableExistsMock = originalEnsureTable
	}()

	// Set up mock for table error
	mockManager.EnsureSavedQueryTableExistsMock = func() error {
		return errors.New("table error")
	}

	// Call the function and check the result
	queries, err := cmd.ListQueriesInternal(mockManager)

	// Assert expectations
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table error")
	assert.Nil(t, queries)
}

func TestListQueriesInternal_GetQueriesError(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Set up mock for get queries error
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return nil, errors.New("queries error")
	}

	// Call the function and check the result
	queries, err := cmd.ListQueriesInternal(mockManager)

	// Assert expectations
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queries error")
	assert.Nil(t, queries)
}

func TestListQueriesInternal_NoQueries(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Set up mock for no queries
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return []data.SavedQueryOptions{}, nil
	}

	// Call the function and check the result
	queries, err := cmd.ListQueriesInternal(mockManager)

	// Assert expectations
	assert.NoError(t, err)
	assert.Empty(t, queries)
}

func TestListQueriesInternal_WithQueries(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Create test queries
	testQueries := []data.SavedQueryOptions{
		{
			Id:           1,
			Name:         "test-query-1",
			QueryString:  "SELECT * FROM test-db.test-container as c WHERE c.id = '123'",
			DatabaseID:   "test-db",
			ContainerID:  "test-container",
			AccountName:  "test-account",
			Description:  "Test query 1",
			DateCreated:  "2023-01-01",
			DateModified: "2023-01-01",
		},
		{
			Id:           2,
			Name:         "test-query-2",
			QueryString:  "SELECT * FROM test-db.test-container as c",
			DatabaseID:   "test-db",
			ContainerID:  "test-container",
			AccountName:  "",
			Description:  "",
			DateCreated:  "2023-01-02",
			DateModified: "2023-01-02",
		},
	}

	// Set up mock for queries
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return testQueries, nil
	}

	// Call the function and check the result
	queries, err := cmd.ListQueriesInternal(mockManager)

	// Assert expectations
	assert.NoError(t, err)
	assert.Equal(t, 2, len(queries))
	assert.Equal(t, "test-query-1", queries[0].Name)
	assert.Equal(t, "test-query-2", queries[1].Name)
	assert.Equal(t, "test-db", queries[0].DatabaseID)
	assert.Equal(t, "test-container", queries[0].ContainerID)
}

func TestListQueryCmd_Success(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Create test queries
	testQueries := []data.SavedQueryOptions{
		{
			Id:           1,
			Name:         "test-query",
			QueryString:  "SELECT * FROM test-db.test-container as c",
			DatabaseID:   "test-db",
			ContainerID:  "test-container",
			AccountName:  "test-account",
			Description:  "Test query",
			DateCreated:  "2023-01-01",
			DateModified: "2023-01-01",
		},
	}

	// Set up mock for queries
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return testQueries, nil
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.ListQueryCmd(sp)

	// Execute the command
	err := cmd.Execute()

	// Command should execute without error
	assert.NoError(t, err)
}

func TestListQueryCmd_NoQueriesFound(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Set up mock for no queries
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return []data.SavedQueryOptions{}, nil
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.ListQueryCmd(sp)

	// Execute the command
	err := cmd.Execute()

	// Command should execute without error
	assert.NoError(t, err)
}

func TestListQueryCmd_Error(t *testing.T) {
	// Save the original mock functions to restore after test
	originalGetQueries := mockManager.GetSavedQueriesMock
	defer func() {
		mockManager.GetSavedQueriesMock = originalGetQueries
	}()

	// Set up mock for error
	mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
		return nil, errors.New("query error")
	}

	// Create service provider with mocked database manager
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
	}

	// Create the command
	cmd := cmd.ListQueryCmd(sp)

	// Execute the command
	err := cmd.Execute()

	// Command should execute without error even if there are internal errors
	assert.NoError(t, err)
}
