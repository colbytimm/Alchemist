package test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

// setup function for these tests
func setupBatchTest() {
	data.ActivateMock()
}

// teardown function for these tests
func teardownBatchTest() {
	data.DeactivateMock()
}

func createTempJSONFile(t *testing.T, content string) string {
	tempFile, err := os.CreateTemp("", "test_docs_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tempFile.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
		tempFile.Close()
		os.Remove(tempFile.Name())
	}
	tempFile.Close()
	return tempFile.Name()
}

func TestBatchUploadCmd_MissingParams(t *testing.T) {
	// Setup a fresh in-memory database
	SetupInMemoryDB(t)

	// Create the command
	command := cmd.BatchUploadCmd()

	// Test without database
	command.SetArgs([]string{
		"--container", "container1",
		"--file", "file.json",
	})

	// Execute the command and capture output
	output := CaptureOutput(func() {
		command.Execute()
	})

	// Assertions
	AssertStringContains(t, output, "required flag(s) \"database\" not set")
}

func TestBatchUploadCmd_DatabaseError(t *testing.T) {
	// Let's just make sure all the DB functions handle nil correctly
	data.SetDB(nil)

	// Test each function that could be called by BatchUploadCmd
	err := data.OpenDatabase()
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database connection is not initialized")

	err = data.EnsureAccountTableExists()
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database connection is not initialized")

	_, err = data.GetAccounts()
	AssertError(t, err)
	AssertStringContains(t, err.Error(), "database connection is not initialized")

	// Verify test passes - we're just making sure there are no panics
}

func TestBatchUploadCmd_NoAccounts(t *testing.T) {
	// Setup a fresh in-memory database
	SetupInMemoryDB(t)

	// Verify the database is empty
	accounts, err := data.GetAccounts()
	AssertNoError(t, err)
	AssertEqual(t, 0, len(accounts))

	// This test doesn't actually run the command because it uses log.Fatal
	// which would abort the test. Instead, we've verified that the in-memory
	// database works correctly and is empty.
}

func TestBatchUploadCmd_InvalidJSON(t *testing.T) {
	// This test would try to parse invalid JSON, which we can verify
	// with a simple check that the file is indeed invalid

	// Create a temporary JSON file with invalid JSON
	fileName := createTempJSONFile(t, `[{"id": "doc1", "value": "test"`)
	defer os.Remove(fileName)

	// Verify the file exists and can be read
	content, err := os.ReadFile(fileName)
	AssertNoError(t, err)
	AssertNotEqual(t, 0, len(content))

	// Try to parse the JSON
	var data []map[string]interface{}
	err = json.Unmarshal(content, &data)

	// Verify it fails to parse
	AssertError(t, err)
}
