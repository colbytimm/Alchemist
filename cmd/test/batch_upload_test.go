package test

import (
	"errors"
	"os"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
)

var mockBatchUpload func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error)

func setupBatchTest() {
	// Setup cosmos mock
	mockCosmos := cosmos.NewMockCosmosManager()
	mockCosmos.BatchUploadMock = func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
		return &cosmos.BatchUploadResult{
			Successful: len(documents),
			Failed:     0,
			TotalRUs:   float64(len(documents)),
		}, nil
	}
}

func createTempJSONFile(t *testing.T, content string) string {
	tempFile, err := os.CreateTemp("", "test_docs_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tempFile.WriteString(content); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	tempFile.Close()
	return tempFile.Name()
}

func TestBatchUpload_MissingParams(t *testing.T) {
	setupBatchTest()

	mockCosmos := cosmos.NewMockCosmosManager()

	// Test empty database ID
	_, err := cmd.BatchUploadInternal(
		"", // account name
		"", // database ID - empty
		"container1",
		"file.json",
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database ID, container ID, and input file are required")

	// Test empty container ID
	_, err = cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"", // container ID - empty
		"file.json",
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database ID, container ID, and input file are required")

	// Test empty input file
	_, err = cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"container1",
		"",    // input file - empty
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database ID, container ID, and input file are required")
}

func TestBatchUpload_DatabaseError(t *testing.T) {
	setupBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"}]`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	mockCosmos := cosmos.NewMockCosmosManager()

	_, err := cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"container1",
		fileName,
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestBatchUpload_NoAccounts(t *testing.T) {
	setupBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"}]`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{}, nil
	}

	mockCosmos := cosmos.NewMockCosmosManager()

	_, err := cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"container1",
		fileName,
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no accounts found")
}

func TestBatchUpload_InvalidJSON(t *testing.T) {
	setupBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "AccountEndpoint=https://test.documents.azure.com:443/;AccountKey=key==;",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	mockCosmos := cosmos.NewMockCosmosManager()

	_, err := cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"container1",
		fileName,
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing JSON")
}

func TestBatchUpload_Success(t *testing.T) {
	setupBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"}]`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               1,
				Name:             "default-account",
				ConnectionString: "AccountEndpoint=https://test.documents.azure.com:443/;AccountKey=key==;",
				Tag:              "test",
				IsDefault:        true,
			},
		}, nil
	}

	mockCosmos := cosmos.NewMockCosmosManager()
	mockCosmos.BatchUploadMock = func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
		return &cosmos.BatchUploadResult{
			Successful: len(documents),
			Failed:     0,
			TotalRUs:   float64(len(documents)),
		}, nil
	}

	result, err := cmd.BatchUploadInternal(
		"", // account name
		"database1",
		"container1",
		fileName,
		100,   // batch size
		true,  // retry
		3,     // max retries
		false, // verbose
		100,   // batch pause ms
		mockManager,
		mockCosmos,
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, result.Successful)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, float64(1), result.TotalRUs)
}
