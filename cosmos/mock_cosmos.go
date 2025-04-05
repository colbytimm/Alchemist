package cosmos

import (
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// MockCosmosManager is a mock implementation of CosmosManager for testing.
type MockCosmosManager struct {
	ConnectMock                func(connectionString string) error
	CrossPartitionQueryMock    func(databaseID, containerID, connectionString, query string, verbose bool) (string, error)
	GetDatabaseIdsMock         func() []string
	GetContainerIdsMock        func(databaseID string) []string
	GetDatabasePropertiesMock  func(databaseID string) *azcosmos.DatabaseProperties
	GetContainerPropertiesMock func(databaseID, containerID string) *azcosmos.ContainerProperties
	CreateDatabaseMock         func(databaseId string) (*azcosmos.DatabaseProperties, error)
	CreateContainerMock        func(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error)
	BatchUploadMock            func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error)
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
		GetDatabaseIdsMock: func() []string {
			return []string{"test-db"}
		},
		GetContainerIdsMock: func(databaseID string) []string {
			return []string{"test-container"}
		},
		GetDatabasePropertiesMock: func(databaseID string) *azcosmos.DatabaseProperties {
			return &azcosmos.DatabaseProperties{ID: databaseID}
		},
		GetContainerPropertiesMock: func(databaseID, containerID string) *azcosmos.ContainerProperties {
			return &azcosmos.ContainerProperties{ID: containerID}
		},
		CreateDatabaseMock: func(databaseId string) (*azcosmos.DatabaseProperties, error) {
			return &azcosmos.DatabaseProperties{ID: databaseId}, nil
		},
		CreateContainerMock: func(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
			return &azcosmos.ContainerProperties{ID: containerId}, nil
		},
		BatchUploadMock: func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
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

func (m *MockCosmosManager) GetDatabaseIds() []string {
	if m.GetDatabaseIdsMock != nil {
		return m.GetDatabaseIdsMock()
	}
	return []string{}
}

func (m *MockCosmosManager) GetContainerIds(databaseID string) []string {
	if m.GetContainerIdsMock != nil {
		return m.GetContainerIdsMock(databaseID)
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

func (m *MockCosmosManager) CreateDatabase(databaseId string) (*azcosmos.DatabaseProperties, error) {
	if m.CreateDatabaseMock != nil {
		return m.CreateDatabaseMock(databaseId)
	}
	return &azcosmos.DatabaseProperties{ID: databaseId}, nil
}

func (m *MockCosmosManager) CreateContainer(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	if m.CreateContainerMock != nil {
		return m.CreateContainerMock(databaseId, containerId, partitionKeyPath)
	}
	return &azcosmos.ContainerProperties{ID: containerId}, nil
}

func (m *MockCosmosManager) BatchUpload(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
	if m.BatchUploadMock != nil {
		return m.BatchUploadMock(databaseId, containerId, connectionString, documents, options)
	}
	return &BatchUploadResult{
		Successful: len(documents),
		Failed:     0,
		TotalRUs:   0,
	}, nil
}
