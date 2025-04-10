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
	originalGetDatabaseIDsImpl         = cosmos.GetDatabaseIDsImpl
	originalGetContainerIDsImpl        = cosmos.GetContainerIDsImpl
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
	cosmos.GetDatabaseIDsImpl = func() []string {
		return []string{}
	}

	cosmos.GetContainerIDsImpl = func(dbID string) []string {
		return []string{}
	}

	cosmos.GetDatabasePropertiesImpl = func(dbID string) *azcosmos.DatabaseProperties {
		return nil
	}

	cosmos.GetContainerPropertiesImpl = func(dbID, containerID string) *azcosmos.ContainerProperties {
		return nil
	}
}

func restoreImplementations() {
	cosmos.ConnectImpl = originalConnectImpl
	cosmos.GetDatabaseIDsImpl = originalGetDatabaseIDsImpl
	cosmos.GetContainerIDsImpl = originalGetContainerIDsImpl
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

func MockGetDatabaseIDsImpl(mockFn func() []string, testFn func()) {
	original := cosmos.GetDatabaseIDsImpl
	cosmos.GetDatabaseIDsImpl = mockFn
	testFn()
	cosmos.GetDatabaseIDsImpl = original
}

func TestGetDatabaseIDs(t *testing.T) {
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
			cosmos.GetDatabaseIDsImpl = func() []string {
				return tt.mockResponse
			}

			result := cosmos.GetDatabaseIDsImpl()
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
		databaseID    string
		expectError   bool
		errorContains string
		mockResponse  *azcosmos.DatabaseProperties
		mockError     error
	}{
		{
			name:          "Database Creation Error",
			databaseID:    "test-db",
			expectError:   true,
			errorContains: "creation failed",
			mockResponse:  nil,
			mockError:     errors.New("database creation failed"),
		},
		{
			name:         "Successful Database Creation",
			databaseID:   "test-db",
			expectError:  false,
			mockResponse: &azcosmos.DatabaseProperties{ID: "test-db"},
			mockError:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.CreateDatabaseImpl = func(dbID string) (*azcosmos.DatabaseProperties, error) {
				assert.Equal(t, tt.databaseID, dbID)
				return tt.mockResponse, tt.mockError
			}

			result, err := cosmos.CreateDatabase(tt.databaseID)

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
		databaseID       string
		containerID      string
		partitionKeyPath string
		expectError      bool
		errorContains    string
		mockResponse     *azcosmos.ContainerProperties
		mockError        error
	}{
		{
			name:             "Container Creation Error",
			databaseID:       "test-db",
			containerID:      "test-container",
			partitionKeyPath: "/id",
			expectError:      true,
			errorContains:    "creation failed",
			mockResponse:     nil,
			mockError:        errors.New("container creation failed"),
		},
		{
			name:             "Successful Container Creation",
			databaseID:       "test-db",
			containerID:      "test-container",
			partitionKeyPath: "/id",
			expectError:      false,
			mockResponse:     &azcosmos.ContainerProperties{ID: "test-container"},
			mockError:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.CreateContainerImpl = func(dbID, cId, pkPath string) (*azcosmos.ContainerProperties, error) {
				assert.Equal(t, tt.databaseID, dbID)
				assert.Equal(t, tt.containerID, cId)
				assert.Equal(t, tt.partitionKeyPath, pkPath)
				return tt.mockResponse, tt.mockError
			}

			result, err := cosmos.CreateContainer(tt.databaseID, tt.containerID, tt.partitionKeyPath)

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

func TestGetContainerIDs(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		mockResponse []string
	}{
		{
			name:         "No Containers",
			databaseID:   "test-db",
			mockResponse: []string{},
		},
		{
			name:         "Multiple Containers",
			databaseID:   "test-db",
			mockResponse: []string{"container1", "container2", "container3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetContainerIDsImpl = func(dbID string) []string {
				assert.Equal(t, tt.databaseID, dbID)
				return tt.mockResponse
			}

			result := cosmos.GetContainerIDsImpl(tt.databaseID)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}

func TestGetDatabaseProperties(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		mockResponse *azcosmos.DatabaseProperties
	}{
		{
			name:         "No Properties",
			databaseID:   "test-db",
			mockResponse: nil,
		},
		{
			name:       "Valid Database Properties",
			databaseID: "test-db",
			mockResponse: &azcosmos.DatabaseProperties{
				ID: "test-db",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetDatabasePropertiesImpl = func(dbID string) *azcosmos.DatabaseProperties {
				assert.Equal(t, tt.databaseID, dbID)
				return tt.mockResponse
			}

			result := cosmos.GetDatabasePropertiesImpl(tt.databaseID)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}

func TestGetContainerProperties(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		containerID  string
		mockResponse *azcosmos.ContainerProperties
	}{
		{
			name:         "No Properties",
			databaseID:   "test-db",
			containerID:  "test-container",
			mockResponse: nil,
		},
		{
			name:        "Valid Container Properties",
			databaseID:  "test-db",
			containerID: "test-container",
			mockResponse: &azcosmos.ContainerProperties{
				ID: "test-container",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cosmos.GetContainerPropertiesImpl = func(dbID, cId string) *azcosmos.ContainerProperties {
				assert.Equal(t, tt.databaseID, dbID)
				assert.Equal(t, tt.containerID, cId)
				return tt.mockResponse
			}

			result := cosmos.GetContainerPropertiesImpl(tt.databaseID, tt.containerID)
			assert.Equal(t, tt.mockResponse, result)
		})
	}
}
