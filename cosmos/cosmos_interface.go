package cosmos

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/charmbracelet/log"
)

// CosmosManager is an interface for cosmos operations that can be mocked in tests.
type CosmosManager interface {
	Connect(connectionString string) error
	CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error)
	GetDatabaseIds() []string
	GetContainerIds(databaseID string) []string
	GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties
	GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties
	CreateDatabase(databaseId string) (*azcosmos.DatabaseProperties, error)
	CreateContainer(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error)
	BatchUpload(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error)
}

// DefaultCosmosManager is the default implementation of CosmosManager.
type DefaultCosmosManager struct {
	client *azcosmos.Client
}

// NewDefaultCosmosManager creates a new DefaultCosmosManager.
func NewDefaultCosmosManager() *DefaultCosmosManager {
	return &DefaultCosmosManager{}
}

func (cm *DefaultCosmosManager) Connect(connectionString string) error {
	cosmosClient, err := azcosmos.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return err
	}

	cm.client = cosmosClient
	return nil
}

func (cm *DefaultCosmosManager) CrossPartitionQuery(databaseID, containerID, connectionString, query string, verbose bool) (string, error) {
	return CrossPartitionQuery(databaseID, containerID, connectionString, query, verbose)
}

func (cm *DefaultCosmosManager) GetDatabaseIds() []string {
	if cm.client == nil {
		return []string{}
	}

	queryPager := cm.client.NewQueryDatabasesPager("select * from dbs d", nil)
	var databaseIds []string
	ctx := context.Background()
	for queryPager.More() {
		queryResponse, err := queryPager.NextPage(ctx)
		if err != nil {
			return []string{}
		}
		for i := range queryResponse.Databases {
			databaseIds = append(databaseIds, queryResponse.Databases[i].ID)
		}
	}
	return databaseIds
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

func (cm *DefaultCosmosManager) CreateDatabase(databaseId string) (*azcosmos.DatabaseProperties, error) {
	if cm.client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	ctx := context.Background()
	databaseResp, err := cm.client.CreateDatabase(ctx, azcosmos.DatabaseProperties{
		ID: databaseId,
	}, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	log.Info("Database created successfully", "id", databaseId)
	return databaseResp.DatabaseProperties, nil
}

func (cm *DefaultCosmosManager) CreateContainer(databaseId, containerId, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	if cm.client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	databaseClient, err := cm.client.NewDatabase(databaseId)
	if err != nil {
		return nil, fmt.Errorf("failed to create database client: %w", err)
	}

	ctx := context.Background()
	containerProperties := azcosmos.ContainerProperties{
		ID: containerId,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
			Paths: []string{partitionKeyPath},
		},
		IndexingPolicy: &azcosmos.IndexingPolicy{
			IndexingMode: azcosmos.IndexingModeConsistent,
			Automatic:    true,
			IncludedPaths: []azcosmos.IncludedPath{
				{
					Path: "/*",
				},
			},
		},
	}

	containerResp, err := databaseClient.CreateContainer(ctx, containerProperties, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	log.Info("Container created successfully", "databaseId", databaseId, "containerId", containerId)
	return containerResp.ContainerProperties, nil
}

func (cm *DefaultCosmosManager) BatchUpload(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
	return BatchUpload(databaseId, containerId, connectionString, documents, options)
}
