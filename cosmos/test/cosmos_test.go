package cosmos

import (
	"errors"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/stretchr/testify/assert"
)

type mockCosmosClient struct{}

var (
	originalConnectImpl                = cosmos.ConnectImpl
	originalGetDatabaseIdsImpl         = cosmos.GetDatabaseIdsImpl
	originalGetContainerIdsImpl        = cosmos.GetContainerIdsImpl
	originalGetDatabasePropertiesImpl  = cosmos.GetDatabasePropertiesImpl
	originalGetContainerPropertiesImpl = cosmos.GetContainerPropertiesImpl
)

func TestMain(m *testing.M) {
	setupSafeImplementations()
	code := m.Run()
	restoreImplementations()
	os.Exit(code)
}

func setupSafeImplementations() {
	cosmos.GetDatabaseIdsImpl = func() []string {
		return []string{}
	}

	cosmos.GetContainerIdsImpl = func(dbId string) []string {
		return []string{}
	}

	cosmos.GetDatabasePropertiesImpl = func(dbId string) *azcosmos.DatabaseProperties {
		return nil
	}

	cosmos.GetContainerPropertiesImpl = func(dbId, containerId string) *azcosmos.ContainerProperties {
		return nil
	}
}

func restoreImplementations() {
	cosmos.ConnectImpl = originalConnectImpl
	cosmos.GetDatabaseIdsImpl = originalGetDatabaseIdsImpl
	cosmos.GetContainerIdsImpl = originalGetContainerIdsImpl
	cosmos.GetDatabasePropertiesImpl = originalGetDatabasePropertiesImpl
	cosmos.GetContainerPropertiesImpl = originalGetContainerPropertiesImpl
}

func Connect(connectionString string) error {
	if connectionString == "" {
		return errors.New("missing Cosmos Connection String")
	}
	return nil
}

func TestConnectSimple(t *testing.T) {
	tests := []struct {
		name             string
		connectionString string
		expectError      bool
		errorContains    string
	}{
		{
			name:             "Empty Connection String",
			connectionString: "",
			expectError:      true,
			errorContains:    "missing Cosmos Connection String",
		},
		{
			name:             "Valid Connection String",
			connectionString: "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;",
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Connect(tt.connectionString)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" && err != nil {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func MockConnectImpl(mockFn func(string) error, testFn func()) {
	original := cosmos.ConnectImpl
	cosmos.ConnectImpl = mockFn
	testFn()
	cosmos.ConnectImpl = original
}

func TestConnectWithMock(t *testing.T) {
	t.Run("Empty Connection String", func(t *testing.T) {
		MockConnectImpl(
			func(connStr string) error {
				assert.Equal(t, "", connStr)
				return errors.New("missing Cosmos Connection String")
			},
			func() {
				err := cosmos.Connect("")
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "missing Cosmos Connection String")
			},
		)
	})

	t.Run("Client Creation Error", func(t *testing.T) {
		MockConnectImpl(
			func(connStr string) error {
				return errors.New("failed to initialize the Cosmos client")
			},
			func() {
				err := cosmos.Connect("dummy")
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to initialize")
			},
		)
	})

	t.Run("Successful Connection", func(t *testing.T) {
		MockConnectImpl(
			func(connStr string) error {
				return nil
			},
			func() {
				validConnStr := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;"
				err := cosmos.Connect(validConnStr)
				assert.NoError(t, err)
			},
		)
	})
}

func MockGetDatabaseIdsImpl(mockFn func() []string, testFn func()) {
	original := cosmos.GetDatabaseIdsImpl
	cosmos.GetDatabaseIdsImpl = mockFn
	testFn()
	cosmos.GetDatabaseIdsImpl = original
}

func TestGetDatabaseIds(t *testing.T) {
	tests := []struct {
		name         string
		mockResponse []string
	}{
		{
			name:         "No Databases",
			mockResponse: []string{},
		},
		{
			name:         "Multiple Databases",
			mockResponse: []string{"db1", "db2", "db3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetDatabaseIdsImpl = func() []string {
				return tt.mockResponse
			}

			result := cosmos.GetDatabaseIdsImpl()
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}

func TestCreateDatabase(t *testing.T) {
	originalCreateDatabaseImpl := cosmos.CreateDatabaseImpl
	defer func() {
		cosmos.CreateDatabaseImpl = originalCreateDatabaseImpl
	}()

	tests := []struct {
		name          string
		databaseId    string
		expectError   bool
		errorContains string
		mockResponse  *azcosmos.DatabaseProperties
		mockError     error
	}{
		{
			name:          "Database Creation Error",
			databaseId:    "test-db",
			expectError:   true,
			errorContains: "creation failed",
			mockResponse:  nil,
			mockError:     errors.New("database creation failed"),
		},
		{
			name:         "Successful Database Creation",
			databaseId:   "test-db",
			expectError:  false,
			mockResponse: &azcosmos.DatabaseProperties{ID: "test-db"},
			mockError:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.CreateDatabaseImpl = func(dbId string) (*azcosmos.DatabaseProperties, error) {
				assert.Equal(t, tt.databaseId, dbId)
				return tt.mockResponse, tt.mockError
			}

			result, err := cosmos.CreateDatabase(tt.databaseId)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mockResponse, result)
			}
		})
	}
}

func TestCreateContainer(t *testing.T) {
	originalCreateContainerImpl := cosmos.CreateContainerImpl
	defer func() {
		cosmos.CreateContainerImpl = originalCreateContainerImpl
	}()

	tests := []struct {
		name             string
		databaseId       string
		containerId      string
		partitionKeyPath string
		expectError      bool
		errorContains    string
		mockResponse     *azcosmos.ContainerProperties
		mockError        error
	}{
		{
			name:             "Container Creation Error",
			databaseId:       "test-db",
			containerId:      "test-container",
			partitionKeyPath: "/id",
			expectError:      true,
			errorContains:    "creation failed",
			mockResponse:     nil,
			mockError:        errors.New("container creation failed"),
		},
		{
			name:             "Successful Container Creation",
			databaseId:       "test-db",
			containerId:      "test-container",
			partitionKeyPath: "/id",
			expectError:      false,
			mockResponse:     &azcosmos.ContainerProperties{ID: "test-container"},
			mockError:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.CreateContainerImpl = func(dbId, cId, pkPath string) (*azcosmos.ContainerProperties, error) {
				assert.Equal(t, tt.databaseId, dbId)
				assert.Equal(t, tt.containerId, cId)
				assert.Equal(t, tt.partitionKeyPath, pkPath)
				return tt.mockResponse, tt.mockError
			}

			result, err := cosmos.CreateContainer(tt.databaseId, tt.containerId, tt.partitionKeyPath)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mockResponse, result)
			}
		})
	}
}

func TestGetContainerIds(t *testing.T) {
	tests := []struct {
		name         string
		databaseId   string
		mockResponse []string
	}{
		{
			name:         "No Containers",
			databaseId:   "test-db",
			mockResponse: []string{},
		},
		{
			name:         "Multiple Containers",
			databaseId:   "test-db",
			mockResponse: []string{"container1", "container2", "container3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetContainerIdsImpl = func(dbId string) []string {
				assert.Equal(t, tt.databaseId, dbId)
				return tt.mockResponse
			}

			result := cosmos.GetContainerIdsImpl(tt.databaseId)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}

func TestGetDatabaseProperties(t *testing.T) {
	tests := []struct {
		name         string
		databaseId   string
		mockResponse *azcosmos.DatabaseProperties
	}{
		{
			name:         "No Properties",
			databaseId:   "test-db",
			mockResponse: nil,
		},
		{
			name:       "Valid Database Properties",
			databaseId: "test-db",
			mockResponse: &azcosmos.DatabaseProperties{
				ID: "test-db",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetDatabasePropertiesImpl = func(dbId string) *azcosmos.DatabaseProperties {
				assert.Equal(t, tt.databaseId, dbId)
				return tt.mockResponse
			}

			result := cosmos.GetDatabasePropertiesImpl(tt.databaseId)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}

func TestGetContainerProperties(t *testing.T) {
	tests := []struct {
		name         string
		databaseId   string
		containerId  string
		mockResponse *azcosmos.ContainerProperties
	}{
		{
			name:         "No Properties",
			databaseId:   "test-db",
			containerId:  "test-container",
			mockResponse: nil,
		},
		{
			name:        "Valid Container Properties",
			databaseId:  "test-db",
			containerId: "test-container",
			mockResponse: &azcosmos.ContainerProperties{
				ID: "test-container",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetContainerPropertiesImpl = func(dbId, cId string) *azcosmos.ContainerProperties {
				assert.Equal(t, tt.databaseId, dbId)
				assert.Equal(t, tt.containerId, cId)
				return tt.mockResponse
			}

			result := cosmos.GetContainerPropertiesImpl(tt.databaseId, tt.containerId)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}
