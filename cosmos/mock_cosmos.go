package cosmos

import (
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// MockCosmosManager is a mock implementation of CosmosManager for testing.
type MockCosmosManager struct {
	ConnectMock                func(connectionString string) error
	CrossPartitionQueryMock    func(databaseID, containerID, connectionString, query string, verbose bool) (string, error)
	GetDatabaseIDsMock         func() []string
	GetContainerIDsMock        func(databaseID string) []string
	GetDatabasePropertiesMock  func(databaseID string) *azcosmos.DatabaseProperties
	GetContainerPropertiesMock func(databaseID, containerID string) *azcosmos.ContainerProperties
	CreateDatabaseMock         func(databaseID string) (*azcosmos.DatabaseProperties, error)
	CreateContainerMock        func(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error)
	BatchUploadMock            func(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error)
}

// NewMockCosmosManager creates a new MockCosmosManager with default mock implementations.
func NewMockCosmosManager() *MockCosmosManager {
	return &MockCosmosManager{
		ConnectMock: func(connectionString string) error {
			return nil
		},
		CrossPartitionQueryMock: func(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
			return "[]", nil
		},
		GetDatabaseIDsMock: func() []string {
			return []string{"test-db"}
		},
		GetContainerIDsMock: func(databaseID string) []string {
			return []string{"test-container"}
		},
		GetDatabasePropertiesMock: func(databaseID string) *azcosmos.DatabaseProperties {
			return &azcosmos.DatabaseProperties{ID: databaseID}
		},
		GetContainerPropertiesMock: func(databaseID, containerID string) *azcosmos.ContainerProperties {
			return &azcosmos.ContainerProperties{ID: containerID}
		},
		CreateDatabaseMock: func(databaseID string) (*azcosmos.DatabaseProperties, error) {
			return &azcosmos.DatabaseProperties{ID: databaseID}, nil
		},
		CreateContainerMock: func(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
			return &azcosmos.ContainerProperties{ID: containerID}, nil
		},
		BatchUploadMock: func(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
			return &BatchUploadResult{
				Successful: len(documents),
				Failed:     0,
				TotalRUs:   0,
			}, nil
		},
	}
}

func (m *MockCosmosManager) Connect(connectionString string) error {
	if m.ConnectMock != nil {
		return m.ConnectMock(connectionString)
	}
	return nil
}

func (m *MockCosmosManager) CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
	if m.CrossPartitionQueryMock != nil {
		return m.CrossPartitionQueryMock(databaseID, containerID, connectionString, query, verbose)
	}
	return "[]", nil
}

func (m *MockCosmosManager) GetDatabaseIDs() []string {
	if m.GetDatabaseIDsMock != nil {
		return m.GetDatabaseIDsMock()
	}
	return []string{}
}

func (m *MockCosmosManager) GetContainerIDs(databaseID string) []string {
	if m.GetContainerIDsMock != nil {
		return m.GetContainerIDsMock(databaseID)
	}
	return []string{}
}

func (m *MockCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	if m.GetDatabasePropertiesMock != nil {
		return m.GetDatabasePropertiesMock(databaseID)
	}
	return nil
}

func (m *MockCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	if m.GetContainerPropertiesMock != nil {
		return m.GetContainerPropertiesMock(databaseID, containerID)
	}
	return nil
}

func (m *MockCosmosManager) CreateDatabase(databaseID string) (*azcosmos.DatabaseProperties, error) {
	if m.CreateDatabaseMock != nil {
		return m.CreateDatabaseMock(databaseID)
	}
	return &azcosmos.DatabaseProperties{ID: databaseID}, nil
}

func (m *MockCosmosManager) CreateContainer(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	if m.CreateContainerMock != nil {
		return m.CreateContainerMock(databaseID, containerID, partitionKeyPath)
	}
	return &azcosmos.ContainerProperties{ID: containerID}, nil
}

func (m *MockCosmosManager) BatchUpload(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
	if m.BatchUploadMock != nil {
		return m.BatchUploadMock(databaseID, containerID, connectionString, documents, options)
	}
	return &BatchUploadResult{
		Successful: len(documents),
		Failed:     0,
		TotalRUs:   0,
	}, nil
}
