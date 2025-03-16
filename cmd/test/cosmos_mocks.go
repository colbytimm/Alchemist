package test

import (
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// MockCosmos is a mock implementation of the cosmos operations for testing
type MockCosmos struct {
	CrossPartitionQueryFunc    func(databaseID, containerID, connectionString, query string, verbose bool) (string, error)
	GetDatabaseIdsFunc         func() []string
	GetContainerIdsFunc        func(databaseID string) []string
	GetDatabasePropertiesFunc  func(databaseID string) *azcosmos.DatabaseProperties
	GetContainerPropertiesFunc func(databaseID, containerID string) *azcosmos.ContainerProperties
}

// CrossPartitionQuery mocks the CrossPartitionQuery function
func (m *MockCosmos) CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
	if m.CrossPartitionQueryFunc != nil {
		return m.CrossPartitionQueryFunc(databaseID, containerID, connectionString, query, verbose)
	}
	return "", nil
}

// GetDatabaseIds mocks the GetDatabaseIds function
func (m *MockCosmos) GetDatabaseIds() []string {
	if m.GetDatabaseIdsFunc != nil {
		return m.GetDatabaseIdsFunc()
	}
	return []string{}
}

// GetContainerIds mocks the GetContainerIds function
func (m *MockCosmos) GetContainerIds(databaseID string) []string {
	if m.GetContainerIdsFunc != nil {
		return m.GetContainerIdsFunc(databaseID)
	}
	return []string{}
}

// GetDatabaseProperties mocks the GetDatabaseProperties function
func (m *MockCosmos) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	if m.GetDatabasePropertiesFunc != nil {
		return m.GetDatabasePropertiesFunc(databaseID)
	}
	return &azcosmos.DatabaseProperties{}
}

// GetContainerProperties mocks the GetContainerProperties function
func (m *MockCosmos) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	if m.GetContainerPropertiesFunc != nil {
		return m.GetContainerPropertiesFunc(databaseID, containerID)
	}
	return &azcosmos.ContainerProperties{}
}
