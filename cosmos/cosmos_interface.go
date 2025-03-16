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

type DefaultCosmosManager struct{}

func (cm *DefaultCosmosManager) CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
	return CrossPartitionQuery(databaseID, containerID, connectionString, query, verbose)
}

func (cm *DefaultCosmosManager) GetDatabaseIds() []string {
	return GetDatabaseIds()
}

func (cm *DefaultCosmosManager) GetContainerIds(databaseID string) []string {
	return GetContainerIds(databaseID)
}

func (cm *DefaultCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	return GetDatabaseProperties(databaseID)
}

func (cm *DefaultCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	return GetContainerProperties(databaseID, containerID)
}
