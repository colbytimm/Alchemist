package cosmos

import (
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// CosmosManager is an interface for cosmos operations that can be mocked in tests
type CosmosManager interface {
	CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error)
	GetDatabaseIds() []string
	GetContainerIds(databaseID string) []string
	GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties
	GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties
}

// DefaultCosmosManager implements the CosmosManager interface with the actual cosmos operations
type DefaultCosmosManager struct{}

// CrossPartitionQuery performs a cross partition query
func (cm *DefaultCosmosManager) CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
	return CrossPartitionQuery(databaseID, containerID, connectionString, query, verbose)
}

// GetDatabaseIds retrieves all database ids
func (cm *DefaultCosmosManager) GetDatabaseIds() []string {
	return GetDatabaseIds()
}

// GetContainerIds retrieves container ids for a database
func (cm *DefaultCosmosManager) GetContainerIds(databaseID string) []string {
	return GetContainerIds(databaseID)
}

// GetDatabaseProperties retrieves database properties
func (cm *DefaultCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	return GetDatabaseProperties(databaseID)
}

// GetContainerProperties retrieves container properties
func (cm *DefaultCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	return GetContainerProperties(databaseID, containerID)
}
