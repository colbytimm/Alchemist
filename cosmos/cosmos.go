package cosmos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/charmbracelet/log"
	"github.com/microsoft/gocosmos"
)

var client *azcosmos.Client

// Variables that can be reassigned for testing.
var (
	ConnectImpl                = Connect
	GetDatabaseIDsImpl         = GetDatabaseIDs
	GetDatabasePropertiesImpl  = GetDatabaseProperties
	GetContainerIDsImpl        = GetContainerIDs
	GetContainerPropertiesImpl = GetContainerProperties
)

// CreateDatabaseFn is a function type for creating a database.
type CreateDatabaseFn func(databaseID string) (*azcosmos.DatabaseProperties, error)

// CreateContainerFn is a function type for creating a container.
type CreateContainerFn func(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error)

// Default implementations.
var defaultCreateDatabase CreateDatabaseFn = func(databaseID string) (*azcosmos.DatabaseProperties, error) {
	if client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	ctx := context.Background()
	databaseResp, err := client.CreateDatabase(ctx, azcosmos.DatabaseProperties{
		ID: databaseID,
	}, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	log.Info("Database created successfully", "id", databaseID)
	return databaseResp.DatabaseProperties, nil
}

var defaultCreateContainer CreateContainerFn = func(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	if client == nil {
		return nil, errors.New("cosmos client is nil")
	}

	databaseClient, err := client.NewDatabase(databaseID)
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

var (
	CreateDatabaseImpl  = defaultCreateDatabase
	CreateContainerImpl = defaultCreateContainer
)

type DatabaseInfo struct {
	ID             string
	ResourceID     string
	SelfLink       string
	ETag           string
	ContainerCount int
}

func Connect(cosmosConnectionString string) error {
	// If connection string is empty, try to load from environment
	if cosmosConnectionString == "" {
		cosmosConnectionString = os.Getenv("COSMOS_CONNECTION_STRING")
		if cosmosConnectionString == "" {
			return fmt.Errorf("missing Cosmos Connection String: provide via parameter or COSMOS_CONNECTION_STRING environment variable")
		}
	}

	cosmosClient, err := azcosmos.NewClientFromConnectionString(cosmosConnectionString, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize the Cosmos client: %w", err)
	}

	client = cosmosClient
	return nil
}

func GetItemIDs(cosmosConnectionString, databaseID, containerID string) []string {
	containerClient, _ := client.NewContainer(databaseID, containerID)
	ctx := context.Background()
	containerInfo, _ := containerClient.Read(ctx, nil)
	partitionKeyValues := containerInfo.ContainerProperties.PartitionKeyDefinition.Paths
	partitionKey := partitionKeyValues[0]
	restClient, err := gocosmos.NewRestClient(nil, cosmosConnectionString)
	if err != nil {
		panic(err)
	}
	partitionKey = strings.ReplaceAll(partitionKey, "/", "")
	queryString := fmt.Sprintf("select c.%s from c", partitionKey)
	queryReq := gocosmos.QueryReq{
		DbName:                databaseID,
		CollName:              containerID,
		Query:                 queryString,
		CrossPartitionEnabled: true,
	}
	response := restClient.QueryDocuments(queryReq)
	docs := response.Documents
	var arrayOfStrings []string

	for _, m := range docs {
		jsonString, err := json.Marshal(m)
		if err != nil {
			log.Error("Error converting map to JSON", "error", err)
			continue
		}
		arrayOfStrings = append(arrayOfStrings, string(jsonString))
	}
	return arrayOfStrings
}

func ReadItem(databaseID, containerID, itemID string) string {
	if client == nil {
		log.Fatal("Cosmos client nil")
	}
	containerClient, err := client.NewContainer(databaseID, containerID)
	if err != nil {
		log.Fatal("Failed to get Cosmos container client", "error", err)
	}

	ctx := context.Background()
	itemResponse, err := containerClient.ReadItem(ctx, azcosmos.NewPartitionKeyString(itemID), itemID, nil)
	if err != nil {
		log.Fatal("Failed to fetch item", "error", err)
	}

	var itemData interface{}
	err = json.Unmarshal(itemResponse.Value, &itemData)
	if err != nil {
		log.Fatal("Failed to unmarshal item response", "error", err)
	}

	jsonString, err := json.MarshalIndent(itemData, "", "    ")
	if err != nil {
		log.Fatal("Failed to marshal item into JSON", "error", err)
	}
	return string(jsonString)
}

func GetDatabaseIDs() []string {
	queryPager := client.NewQueryDatabasesPager("select * from dbs d", nil)
	var databaseIDs []string
	ctx := context.Background()
	for queryPager.More() {
		queryResponse, err := queryPager.NextPage(ctx)
		if err != nil {
			var responseErr *azcore.ResponseError
			panic(responseErr)
		}
		for i := range queryResponse.Databases {
			databaseIDs = append(databaseIDs, queryResponse.Databases[i].ID)
		}
	}
	return databaseIDs
}

func GetContainerIDs(databaseID string) []string {
	databaseClient, err := client.NewDatabase(databaseID)
	if err != nil {
		log.Fatal("Failed to get Cosmos database client", "error", err)
	}

	if err != nil {
		panic(err)
	}

	queryPager := databaseClient.NewQueryContainersPager("select * from containers c", nil)
	var containerIDs []string
	ctx := context.Background()
	for queryPager.More() {
		queryResponse, err := queryPager.NextPage(ctx)
		if err != nil {
			var responseErr *azcore.ResponseError
			panic(responseErr)
		}
		for i := range queryResponse.Containers {
			containerIDs = append(containerIDs, queryResponse.Containers[i].ID)
		}
	}
	return containerIDs
}

func GetDatabaseProperties(databaseID string) *azcosmos.DatabaseProperties {
	database, _ := client.NewDatabase(databaseID)
	ctx := context.Background()
	databaseInfo, _ := database.Read(ctx, nil)
	return databaseInfo.DatabaseProperties
}

func GetContainerProperties(databaseID, containerID string) *azcosmos.ContainerProperties {
	if client == nil {
		log.Fatal("Cosmos client is nil")
	}

	containerClient, err := client.NewContainer(databaseID, containerID)
	if err != nil {
		log.Fatal("Failed to get Cosmos container client", "error", err)
	}

	ctx := context.Background()
	containerInfo, err := containerClient.Read(ctx, nil)
	if err != nil {
		log.Fatal("Failed to read container", "error", err)
	}

	return containerInfo.ContainerProperties
}

func CheckContainerAccess(databaseID, containerID string) error {
	if client == nil {
		return errors.New("cosmos client is nil")
	}

	databaseClient, err := client.NewDatabase(databaseID)
	if err != nil {
		return fmt.Errorf("failed to create database client: %w", err)
	}

	ctx := context.Background()
	_, err = databaseClient.Read(ctx, nil)
	if err != nil {
		return fmt.Errorf("database '%s' does not exist or is not accessible: %w", databaseID, err)
	}

	containerClient, err := client.NewContainer(databaseID, containerID)
	if err != nil {
		return fmt.Errorf("failed to create container client: %w", err)
	}

	_, err = containerClient.Read(ctx, nil)
	if err != nil {
		return fmt.Errorf("container '%s' does not exist or is not accessible: %w", containerID, err)
	}

	return nil
}

func CreateTestDocument(databaseID, containerID string) (string, error) {
	if client == nil {
		return "", errors.New("cosmos client is nil")
	}

	containerClient, err := client.NewContainer(databaseID, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to create container client: %w", err)
	}

	timestamp := time.Now().Format(time.RFC3339)
	testDoc := map[string]interface{}{
		"id":        fmt.Sprintf("test-document-%s", timestamp),
		"name":      "Test Document",
		"createdAt": timestamp,
		"isTest":    true,
		"data": map[string]interface{}{
			"value":       42,
			"description": "This is a test document created by Alchemist",
		},
	}

	jsonData, err := json.Marshal(testDoc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal test document: %w", err)
	}

	log.Info("Creating document", "id", testDoc["id"])
	log.Info("Document content", "content", string(jsonData))

	ctx := context.Background()
	pk := azcosmos.NewPartitionKeyString(testDoc["id"].(string))
	resp, err := containerClient.CreateItem(ctx, pk, jsonData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create test document: %w", err)
	}

	log.Info("Document created successfully", "requestCharge", resp.RequestCharge)

	log.Info("Verifying document creation by reading it back...")
	itemResponse, err := containerClient.ReadItem(ctx, pk, testDoc["id"].(string), nil)
	if err != nil {
		return "", fmt.Errorf("document was created but could not be read back: %w", err)
	}

	var readDoc map[string]interface{}
	err = json.Unmarshal(itemResponse.Value, &readDoc)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal read document: %w", err)
	}

	log.Info("Document read successfully", "id", readDoc["id"])

	return testDoc["id"].(string), nil
}

func CreateDatabase(databaseID string) (*azcosmos.DatabaseProperties, error) {
	return CreateDatabaseImpl(databaseID)
}

func CreateContainer(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
	return CreateContainerImpl(databaseID, containerID, partitionKeyPath)
}
