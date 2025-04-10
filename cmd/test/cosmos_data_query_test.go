package test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/stretchr/testify/assert"
)

var mockCrossPartitionQuery func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error)

var originalCrossPartitionQuery = cmd.CrossPartitionQueryImpl

type mockCosmosManager struct {
	CrossPartitionQueryMock func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error)
}

func (m *mockCosmosManager) Connect(connectionString string) error {
	return nil
}

func (m *mockCosmosManager) CrossPartitionQuery(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
	if m.CrossPartitionQueryMock != nil {
		return m.CrossPartitionQueryMock(databaseID, containerID, connectionString, customQuery, verbose)
	}
	return "", errors.New("not implemented")
}

func (m *mockCosmosManager) GetDatabaseIDs() []string {
	return []string{}
}

func (m *mockCosmosManager) GetContainerIDs(databaseID string) []string {
	return []string{}
}

func (m *mockCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	return nil
}

func (m *mockCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	return nil
}

func (m *mockCosmosManager) CreateDatabase(databaseID string) (*azcosmos.DatabaseProperties, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCosmosManager) CreateContainer(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCosmosManager) BatchUpload(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
	return nil, errors.New("not implemented")
}

var mockCosmos = &mockCosmosManager{}

func setupCosmosTest() {
	mockCrossPartitionQuery = func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
		return "", errors.New("unimplemented mock")
	}

	cmd.CrossPartitionQueryImpl = func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
		return mockCrossPartitionQuery(databaseID, containerID, connectionString, customQuery, verbose)
	}

	mockCosmos.CrossPartitionQueryMock = func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
		return mockCrossPartitionQuery(databaseID, containerID, connectionString, customQuery, verbose)
	}
}

func resetCosmosTest() {
	cmd.CrossPartitionQueryImpl = originalCrossPartitionQuery
}

func TestQueryAccount_DatabaseError(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	_, err := cmd.QueryAccountInternal(
		"", // empty account name, use default
		"SELECT * FROM test-db.test-container as c WHERE c.data.value = 42",
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestQueryAccount_TableError(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return errors.New("table error")
	}

	_, err := cmd.QueryAccountInternal(
		"", // empty account name, use default
		"SELECT * FROM test-db.test-container as c WHERE c.data.value = 42",
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "table error")
}

func TestQueryAccount_GetAccountsError(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return nil, errors.New("accounts error")
	}

	_, err := cmd.QueryAccountInternal(
		"", // empty account name, use default
		"SELECT * FROM test-db.test-container as c WHERE c.data.value = 42",
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "accounts error")
}

func TestQueryAccount_NoAccounts(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{}, nil
	}

	_, err := cmd.QueryAccountInternal(
		"", // empty account name, use default
		"SELECT * FROM test-db.test-container as c",
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no accounts found")
}

func TestQueryAccount_AccountNotFound(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "existing-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	_, err := cmd.QueryAccountInternal(
		"non-existent-account",
		"SELECT * FROM test-db.test-container as c WHERE c.data.value = 42",
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account not found")
}

func TestQueryAccount_MissingQuery(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	_, err := cmd.QueryAccountInternal(
		"",    // empty account name, use default
		"",    // empty query
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing query")
}

func TestQueryAccount_ListAllMissingParams(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	_, err := cmd.QueryAccountInternal(
		"",   // empty account name, use default
		"",   // empty query
		true, // list-all mode
		"",   // missing database ID
		"",   // missing container ID
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing parameters")
}

func TestQueryAccount_InvalidQuery(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	// Mock the query execution with an error
	mockCrossPartitionQuery = func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
		return "", errors.New("invalid query")
	}

	_, err := cmd.QueryAccountInternal(
		"",              // empty account name, use default
		"INVALID QUERY", // invalid query
		false,           // not list-all
		"",              // database ID not needed with query
		"",              // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid query")
}

func TestQueryAccount_Success(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Configure mock manager
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	// Mock the query execution with a success
	mockCrossPartitionQuery = func(databaseID, containerID, connectionString, customQuery string, verbose bool) (string, error) {
		// Return sample JSON result
		return `[{"id": "doc1", "value": 42}]`, nil
	}

	result, err := cmd.QueryAccountInternal(
		"", // empty account name, use default
		"SELECT * FROM test-db.test-container as c", // valid query
		false, // not list-all
		"",    // database ID not needed with query
		"",    // container ID not needed with query
		mockManager,
		mockCosmos,
	)

	// Assertions
	assert.NoError(t, err)
	assert.Contains(t, result, "doc1")
	assert.Contains(t, result, "42")
}

func TestQueryAccountCmd(t *testing.T) {
	setupCosmosTest()
	defer resetCosmosTest()

	// Create a mock service provider
	sp := &services.ServiceProvider{
		DatabaseManager: mockManager,
		CosmosManager:   mockCosmos,
	}

	// Configure the mock manager for a successful query
	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "test-connection-string",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	// Mock the query execution
	mockCrossPartitionQuery = func(_, _, _, _ string, _ bool) (string, error) {
		return `{"items":[{"id":"1","name":"test"}]}`, nil
	}

	// Test with query flag
	t.Run("with query flag", func(t *testing.T) {
		// Create a new command for this test
		queryCmd := cmd.CosmosDataQueryCmd(sp)

		// Redirect standard output to capture the query results
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Set args for the command
		queryCmd.SetArgs([]string{"--query", "SELECT * FROM testdb.testcontainer as c"})

		// Execute the command
		err := queryCmd.Execute()

		// Close the writer to complete the pipe
		w.Close()

		// Read captured output
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		// Restore stdout
		os.Stdout = oldStdout

		// Assertions
		assert.NoError(t, err)
		assert.Contains(t, buf.String(), `{"items":[{"id":"1","name":"test"}]}`)
	})

	// Test with list-all flag
	t.Run("with list-all flag", func(t *testing.T) {
		// Create a new command for this test
		queryCmd := cmd.CosmosDataQueryCmd(sp)

		// Redirect standard output to capture the query results
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Set args for the command
		queryCmd.SetArgs([]string{"--list-all", "--database", "testdb", "--container", "testcontainer"})

		// Execute the command
		err := queryCmd.Execute()

		// Close the writer to complete the pipe
		w.Close()

		// Read captured output
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		// Restore stdout
		os.Stdout = oldStdout

		// Assertions
		assert.NoError(t, err)
		assert.Contains(t, buf.String(), `{"items":[{"id":"1","name":"test"}]}`)
	})
}

func TestFormatOutput(t *testing.T) {
	// Test JSON output format (default passthrough)
	t.Run("JSON format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.JSON)
		assert.NoError(t, err)
		assert.Equal(t, input, output)
	})

	// Test RAW output format
	t.Run("RAW format", func(t *testing.T) {
		input := `{"items": [{"id": "1", "name": "test"}]}`
		expectedOutput := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.RAW)
		assert.NoError(t, err)
		assert.Equal(t, expectedOutput, output)
	})

	// Test RAW format with invalid JSON
	t.Run("RAW format with invalid JSON", func(t *testing.T) {
		input := `{"items": [{"id": "1", "name": "test"`
		_, err := cmd.FormatOutput(input, cmd.RAW)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error parsing JSON")
	})

	// Test TABLE format (not fully implemented)
	t.Run("TABLE format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.TABLE)
		assert.NoError(t, err)
		assert.Contains(t, output, "Table formatting not fully implemented yet")
		assert.Contains(t, output, input)
	})

	// Test default format (same as JSON)
	t.Run("Default format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, "unknown")
		assert.NoError(t, err)
		assert.Equal(t, input, output)
	})
}
