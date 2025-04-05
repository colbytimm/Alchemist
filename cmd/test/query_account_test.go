package test

import (
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
)

var mockCrossPartitionQuery func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error)

var originalCrossPartitionQuery = cmd.CrossPartitionQueryImpl

type mockCosmosManager struct {
	CrossPartitionQueryMock func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error)
}

func (m *mockCosmosManager) Connect(connectionString string) error {
	return nil
}

func (m *mockCosmosManager) CrossPartitionQuery(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
	if m.CrossPartitionQueryMock != nil {
		return m.CrossPartitionQueryMock(databaseId, containerId, connectionString, customQuery, verbose)
	}
	return "", errors.New("not implemented")
}

func (m *mockCosmosManager) GetDatabaseIds() []string {
	return []string{}
}

func (m *mockCosmosManager) GetContainerIds(databaseID string) []string {
	return []string{}
}

func (m *mockCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	return nil
}

func (m *mockCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	return nil
}

func (m *mockCosmosManager) CreateDatabase(databaseId string) (*azcosmos.DatabaseProperties, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCosmosManager) CreateContainer(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCosmosManager) BatchUpload(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
	return nil, errors.New("not implemented")
}

var mockCosmos = &mockCosmosManager{}

func setupCosmosTest() {
	mockCrossPartitionQuery = func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
		return "", errors.New("unimplemented mock")
	}

	cmd.CrossPartitionQueryImpl = func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
		return mockCrossPartitionQuery(databaseId, containerId, connectionString, customQuery, verbose)
	}

	mockCosmos.CrossPartitionQueryMock = func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
		return mockCrossPartitionQuery(databaseId, containerId, connectionString, customQuery, verbose)
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
	mockCrossPartitionQuery = func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
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
	mockCrossPartitionQuery = func(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
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
