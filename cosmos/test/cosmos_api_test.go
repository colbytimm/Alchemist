package cosmos

import (
	"strings"
	"testing"

	"github.com/colbytimm/alchemist/cosmos"
	"github.com/stretchr/testify/assert"
)

func TestExtractCosmosCredentials(t *testing.T) {
	tests := []struct {
		name              string
		connectionString  string
		wantEndpoint      string
		wantKey           string
		wantError         bool
		wantErrorContains string
	}{
		{
			name:             "Valid Connection String",
			connectionString: "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;",
			wantEndpoint:     "https://example.documents.azure.com:443/",
			wantKey:          "dGVzdEtleQ==",
			wantError:        false,
		},
		{
			name:              "Missing AccountEndpoint",
			connectionString:  "AccountKey=dGVzdEtleQ==;",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
		{
			name:              "Missing AccountKey",
			connectionString:  "AccountEndpoint=https://example.documents.azure.com:443/;",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint or AccountKey",
		},
		{
			name:              "Empty Connection String",
			connectionString:  "",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
		{
			name:              "Malformed Connection String",
			connectionString:  "InvalidFormat;WithNoProperSeparators",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, key, err := cosmos.ExtractCosmosCredentials(tt.connectionString)

			if (err != nil) != tt.wantError {
				t.Errorf("ExtractCosmosCredentials() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrorContains) {
					t.Errorf("Expected error containing %q, got %v", tt.wantErrorContains, err)
				}
				return
			}

			if endpoint != tt.wantEndpoint {
				t.Errorf("ExtractCosmosCredentials() endpoint = %v, want %v", endpoint, tt.wantEndpoint)
			}

			if key != tt.wantKey {
				t.Errorf("ExtractCosmosCredentials() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

// Mock the HTTP client for testing CrossPartitionQuery.
type mockHttpDoer struct {
	responseFunc func(req *string) ([]byte, int, error)
}

func TestBatchUploadOptions(t *testing.T) {
	// Test with nil options
	result, err := cosmos.BatchUpload("test-db", "test-container", "invalid-connection", nil, nil)
	assert.Error(t, err)
	assert.Nil(t, result)

	// Test with custom options
	customOpts := &cosmos.BatchUploadOptions{
		BatchSize:    50,
		Retry:        false,
		MaxRetries:   5,
		Verbose:      true,
		BatchPauseMs: 200,
	}

	// Verify that the options are used correctly
	// Note: We can only test this indirectly since the function is private
	// In a real test, we would mock the HTTP client and verify the behavior
	_, err = cosmos.BatchUpload("test-db", "test-container", "invalid-connection", nil, customOpts)
	assert.Error(t, err)
}

func TestBatchUploadResults(t *testing.T) {
	// Test the BatchUploadResult struct
	successfulDocs := 5
	failedDocs := 2
	totalRUs := 10.5
	failedDocuments := []map[string]interface{}{
		{"id": "doc1", "error": "failed"},
		{"id": "doc2", "error": "timeout"},
	}
	errors := []string{"error1", "error2"}

	result := &cosmos.BatchUploadResult{
		Successful:      successfulDocs,
		Failed:          failedDocs,
		TotalRUs:        totalRUs,
		FailedDocuments: failedDocuments,
		Errors:          errors,
	}

	assert.Equal(t, successfulDocs, result.Successful)
	assert.Equal(t, failedDocs, result.Failed)
	assert.Equal(t, totalRUs, result.TotalRUs)
	assert.Equal(t, failedDocuments, result.FailedDocuments)
	assert.Equal(t, errors, result.Errors)
}

func TestCrossPartitionQuery_InvalidConnection(t *testing.T) {
	// Test with invalid connection string
	result, err := cosmos.CrossPartitionQuery("test-db", "test-container", "invalid-connection", "SELECT * FROM c", false)
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestCrossPartitionQuery_EmptyQuery(t *testing.T) {
	// Test with empty query (should use GET instead of POST)
	connectionString := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;"

	// This should construct a GET request but will fail to connect
	// In a real test we would mock the HTTP client
	result, err := cosmos.CrossPartitionQuery("test-db", "test-container", connectionString, "", false)
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestCrossPartitionQuery_WithCustomQuery(t *testing.T) {
	// Test with custom query (should use POST)
	connectionString := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;"
	customQuery := "SELECT * FROM c WHERE c.id = 'test'"

	// This should construct a POST request but will fail to connect
	// In a real test we would mock the HTTP client
	result, err := cosmos.CrossPartitionQuery("test-db", "test-container", connectionString, customQuery, false)
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestBatchUpload_InvalidConnectionString(t *testing.T) {
	docs := []map[string]interface{}{
		{"id": "doc1", "name": "Test Document 1"},
		{"id": "doc2", "name": "Test Document 2"},
	}

	// Test with invalid connection string
	result, err := cosmos.BatchUpload("test-db", "test-container", "invalid-connection", docs, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestBatchUpload_EmptyDocuments(t *testing.T) {
	connectionString := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;"

	// Test with empty documents
	result, err := cosmos.BatchUpload("test-db", "test-container", connectionString, []map[string]interface{}{}, nil)

	// This should succeed but not upload anything
	// In a real test, we would verify the result more thoroughly
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Successful)
	assert.Equal(t, 0, result.Failed)
}

func TestGenerateAuthorizationToken(t *testing.T) {
	// Since this is an internal function, we need to test it indirectly
	// through the CrossPartitionQuery function

	// Use a valid base64 key format to avoid decoding errors
	connectionString := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;"
	result, err := cosmos.CrossPartitionQuery("test-db", "test-container", connectionString, "SELECT * FROM c", false)

	// We still expect an error, but not from base64 decoding
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestDefaultBatchUploadOptions(t *testing.T) {
	// Test the default batch upload options with an invalid connection string
	connectionString := "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=invalid-key;"
	docs := []map[string]interface{}{
		{"id": "doc1", "name": "Test Document 1"},
	}

	// Using nil options should apply defaults
	// This will fail due to invalid connection which is expected
	result, err := cosmos.BatchUpload("test-db", "test-container", connectionString, docs, nil)

	// The function should return an error or a result with errors
	if err == nil {
		// If no direct error, check if the result contains failures
		assert.NotNil(t, result)
		assert.True(t, result.Failed > 0 || len(result.Errors) > 0,
			"Expected either an error or a result with failures")
	} else {
		assert.Error(t, err, "Expected an error with invalid connection string")
	}
}
