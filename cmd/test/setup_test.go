package test

import (
	"os"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

var mockManager *data.MockDatabaseManager
var originalValidateQuery func(string) error

func TestMain(m *testing.M) {
	setupTestEnvironment()

	code := m.Run()

	teardownTestEnvironment()

	os.Exit(code)
}

func setupTestEnvironment() {
	mockManager = data.NewMockDatabaseManager()
	data.SetDefaultManager(mockManager)
	originalValidateQuery = cmd.ValidateQueryFunc
}

func teardownTestEnvironment() {
	data.SetDefaultManager(&data.SQLiteManager{})
	cmd.ValidateQueryFunc = originalValidateQuery
}
