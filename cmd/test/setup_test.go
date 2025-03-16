package test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/colbytimm/alchemist/data"
	_ "github.com/mattn/go-sqlite3"
)

var originalDB = data.GetDB()

func TestMain(m *testing.M) {
	setupTestEnvironment()

	// Run all tests
	code := m.Run()

	teardownTestEnvironment()

	os.Exit(code)
}

// SetupInMemoryDB creates a fresh in-memory database with the account schema
// This should be used for each test to ensure tests don't interfere with each other
func SetupInMemoryDB(t testing.TB) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory database: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS account (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"connectionString" TEXT,
		"tag" TEXT,
		"isDefault" BOOLEAN
	);`)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create account table: %v", err)
	}

	data.SetDB(db)

	t.Cleanup(func() {
		db.Close()
		data.SetDB(nil)
	})
}

// AddTestAccount adds a test account to the current database
func AddTestAccount(t testing.TB, name, connectionString, tag string, isDefault bool) {
	db := data.GetDB()
	if db == nil {
		t.Fatal("Database is nil, call SetupInMemoryDB first")
	}

	_, err := db.Exec(
		"INSERT INTO account (name, connectionString, tag, isDefault) VALUES (?, ?, ?, ?)",
		name, connectionString, tag, isDefault,
	)
	if err != nil {
		t.Fatalf("Failed to add test account: %v", err)
	}
}

func setupTestEnvironment() {
	// Ensure no real database is used in tests
	data.SetDB(nil)
}

func teardownTestEnvironment() {
	data.SetDB(originalDB)
}
