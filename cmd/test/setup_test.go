package test

import (
	"os"
	"testing"

	"github.com/colbytimm/alchemist/data"
)

var mockManager *data.MockDatabaseManager

func TestMain(m *testing.M) {
	setupTestEnvironment()

	code := m.Run()

	teardownTestEnvironment()

	os.Exit(code)
}

func setupTestEnvironment() {
	mockManager = data.NewMockDatabaseManager()
	data.SetDefaultManager(mockManager)
}

func teardownTestEnvironment() {
	data.SetDefaultManager(&data.SQLiteManager{})
}
