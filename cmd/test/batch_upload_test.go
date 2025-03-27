package test

import (
	"errors"
	"os"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
)

var mockBatchUpload func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error)
var originalBatchUpload = cmd.BatchUploadImpl
var originalConnectImpl = cmd.ConnectImpl

func setupBatchTest() {
	cmd.ConnectImpl = func(connectionString string) error {
		return nil
	}

	mockBatchUpload = func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
		return &cosmos.BatchUploadResult{
			Successful: len(documents),
			Failed:     0,
			TotalRUs:   float64(len(documents)),
		}, nil
	}

	cmd.BatchUploadImpl = func(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
		return mockBatchUpload(databaseId, containerId, connectionString, documents, options)
	}
}

func teardownBatchTest() {
	cmd.BatchUploadImpl = originalBatchUpload
	cmd.ConnectImpl = originalConnectImpl
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
	defer teardownBatchTest()

	options := &cosmos.BatchUploadOptions{
		Verbose: false,
	}

	_, err := cmd.BatchUploadInternal("", "", "container1", "file.json", options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database ID is required")

	_, err = cmd.BatchUploadInternal("", "database1", "", "file.json", options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "container ID is required")

	_, err = cmd.BatchUploadInternal("", "database1", "container1", "", options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "JSON file is required")
}

func TestBatchUpload_DatabaseError(t *testing.T) {
	setupBatchTest()
	defer teardownBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"}]`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	options := &cosmos.BatchUploadOptions{
		Verbose: false,
	}

	_, err := cmd.BatchUploadInternal("", "database1", "container1", fileName, options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database error")
}

func TestBatchUpload_NoAccounts(t *testing.T) {
	setupBatchTest()
	defer teardownBatchTest()

	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"}]`)
	defer os.Remove(fileName)

	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{}, nil
	}

	options := &cosmos.BatchUploadOptions{
		Verbose: false,
	}

	_, err := cmd.BatchUploadInternal("", "database1", "container1", fileName, options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "no accounts found")
}

func TestBatchUpload_InvalidJSON(t *testing.T) {
	setupBatchTest()
	defer teardownBatchTest()

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

	options := &cosmos.BatchUploadOptions{
		Verbose: false,
	}

	_, err := cmd.BatchUploadInternal("", "database1", "container1", fileName, options)
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "failed to parse JSON file")
}

func TestBatchUpload_Success(t *testing.T) {
	setupBatchTest()
	defer teardownBatchTest()

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

	options := &cosmos.BatchUploadOptions{
		Verbose: false,
	}

	result, err := cmd.BatchUploadInternal("", "database1", "container1", fileName, options)
	AssertNoError(t, err)
	AssertEqual(t, 1, result.Successful)
	AssertEqual(t, 0, result.Failed)
	AssertEqual(t, float64(1), result.TotalRUs)
}
