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
	GetDatabaseIDs() []string
	GetContainerIDs(databaseID string) []string
	GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties
	GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties
	CreateDatabase(databaseID string) (*azcosmos.DatabaseProperties, error)
	CreateContainer(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error)
	BatchUpload(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error)
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

func (cm *DefaultCosmosManager) GetDatabaseIDs() []string {
	if cm.client == nil {
		return []string{}
	}

	queryPager := cm.client.NewQueryDatabasesPager("select * from dbs d", nil)
	var databaseIDs []string
	ctx := context.Background()
	for queryPager.More() {
		queryResponse, err := queryPager.NextPage(ctx)
		if err != nil {
			return []string{}
		}
		for i := range queryResponse.Databases {
			databaseIDs = append(databaseIDs, queryResponse.Databases[i].ID)
		}
	}
	return databaseIDs
}

func (cm *DefaultCosmosManager) GetContainerIDs(databaseID string) []string {
	return GetContainerIDs(databaseID)
}

func (cm *DefaultCosmosManager) GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	return GetDatabaseProperties(databaseID)
}

func (cm *DefaultCosmosManager) GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	return GetContainerProperties(databaseID, containerID)
}

func (cm *DefaultCosmosManager) CreateDatabase(databaseID string) (*azcosmos.DatabaseProperties, error) {
	if cm.client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	ctx := context.Background()
	databaseResp, err := cm.client.CreateDatabase(ctx, azcosmos.DatabaseProperties{
		ID: databaseID,
	}, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	log.Info("Database created successfully", "id", databaseID)
	return databaseResp.DatabaseProperties, nil
}

func (cm *DefaultCosmosManager) CreateContainer(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	if cm.client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	databaseClient, err := cm.client.NewDatabase(databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to create database client: %w", err)
	}

	ctx := context.Background()
	containerProperties := azcosmos.ContainerProperties{
		ID: containerID,
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

	log.Info("Container created successfully", "databaseID", databaseID, "containerID", containerID)
	return containerResp.ContainerProperties, nil
}

func (cm *DefaultCosmosManager) BatchUpload(databaseID, containerID, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
	return BatchUpload(databaseID, containerID, connectionString, documents, options)
}
