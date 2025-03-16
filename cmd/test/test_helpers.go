package test

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

// CaptureOutput captures stdout, stderr, and log output during function execution
func CaptureOutput(fn func()) string {
	// Capture stdout/stderr
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// Capture logs
	var logBuf bytes.Buffer
	originalLogger := log.Default()
	testLogger := log.New(os.Stderr)
	testLogger.SetOutput(&logBuf)
	log.SetDefault(testLogger)

	// Run the function
	fn()

	// Restore standard output
	w.Close()
	os.Stdout = originalStdout
	os.Stderr = originalStderr

	// Restore logger
	log.SetDefault(originalLogger)

	// Get output from both sources
	var stdBuf bytes.Buffer
	io.Copy(&stdBuf, r)

	// Combine the outputs
	return stdBuf.String() + logBuf.String()
}

// SetupTestDB creates an in-memory test database with the schema
func SetupTestDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS account (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"connectionString" TEXT,
		"tag" TEXT,
		"isDefault" BOOLEAN
	);`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// InsertTestAccount adds a test account to the database
func InsertTestAccount(db *sql.DB, name, connectionString, tag string, isDefault bool) error {
	_, err := db.Exec(
		"INSERT INTO account (name, connectionString, tag, isDefault) VALUES (?, ?, ?, ?)",
		name, connectionString, tag, isDefault,
	)
	return err
}

func AssertStringContains(t *testing.T, output, contains string) {
	if !strings.Contains(output, contains) {
		t.Errorf("Expected output to contain %q, got %q", contains, output)
	}
}

func AssertStringNotContains(t *testing.T, output, contains string) {
	if strings.Contains(output, contains) {
		t.Errorf("Expected output to not contain %q, but it did", contains)
	}
}

func AssertEqual(t *testing.T, expected, actual interface{}) {
	if expected != actual {
		t.Errorf("Expected %v, got %v", expected, actual)
	}
}

func AssertNotEqual(t *testing.T, expected, actual interface{}) {
	if expected == actual {
		t.Errorf("Expected %v to not equal %v, but it did", expected, actual)
	}
}

func AssertNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func AssertError(t *testing.T, err error) {
	if err == nil {
		t.Errorf("Expected an error, got nil")
	}
}
