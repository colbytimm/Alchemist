package data

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type DatabaseManager interface {
	OpenDatabase() error
	EnsureAccountTableExists() error
	InsertAccount(options *AccountOptions) (AccountOptions, error)
	GetAccountByName(name string) (AccountOptions, error)
	GetAccounts() ([]AccountOptions, error)
	DeleteAccountByName(name string) error
	UpdateDefaultItem(name string) error
	EnsureSavedQueryTableExists() error
	SaveQuery(options *SavedQueryOptions) (SavedQueryOptions, error)
	GetSavedQueries() ([]SavedQueryOptions, error)
	GetSavedQueryByName(name string) (SavedQueryOptions, error)
	DeleteSavedQueryByName(name string) error
}

type SQLiteManager struct {
	db *sql.DB
}

var defaultManager DatabaseManager = &SQLiteManager{}

func GetDefaultManager() DatabaseManager {
	return defaultManager
}

func SetDefaultManager(manager DatabaseManager) {
	defaultManager = manager
}

// GetDB returns the current database connection for testing purposes.
func GetDB() *sql.DB {
	if sqliteManager, ok := defaultManager.(*SQLiteManager); ok {
		return sqliteManager.db
	}
	return nil
}

// SetDB sets the database connection for testing purposes.
func SetDB(database *sql.DB) {
	if sqliteManager, ok := defaultManager.(*SQLiteManager); ok {
		sqliteManager.db = database
	}
}

type AccountOptions struct {
	Id               int
	Name             string
	ConnectionString string
	Tag              string
	IsDefault        bool
}

func (m *SQLiteManager) OpenDatabase() error {
	var err error
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting home directory: %w", err)
	}

	dataDir := filepath.Join(homeDir, ".alchemist")
	err = os.MkdirAll(dataDir, 0o700)
	if err != nil {
		return fmt.Errorf("error creating data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "alchemist.db")
	m.db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}

	return nil
}

func (m *SQLiteManager) CreateAccountTable() error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS account (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"connectionString" TEXT,
		"tag" TEXT,
		"isDefault" BOOLEAN
	  );`

	statement, err := m.db.Prepare(createTableSQL)
	if err != nil {
		return err
	}

	_, err = statement.Exec()
	if err != nil {
		return err
	}

	return nil
}

func (m *SQLiteManager) EnsureAccountTableExists() error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	var tableName string
	err := m.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='account'").Scan(&tableName)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return m.CreateAccountTable()
		}
		return err
	}

	return nil
}

func (m *SQLiteManager) InsertAccount(options *AccountOptions) (AccountOptions, error) {
	if m.db == nil {
		return AccountOptions{}, fmt.Errorf("database connection is not initialized")
	}

	var totalAccounts int
	err := m.db.QueryRow("SELECT COUNT(*) FROM account WHERE name = ?", options.Name).Scan(&totalAccounts)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error checking account count: %w", err)
	}

	if totalAccounts > 0 {
		return AccountOptions{}, fmt.Errorf("account with name '%s' already exists", options.Name)
	}

	insertStatement, err := m.db.Prepare("INSERT INTO account(name, connectionString, tag, isDefault) values(?, ?, ?, ?)")
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error preparing insert statement: %w", err)
	}
	defer insertStatement.Close()

	result, err := insertStatement.Exec(options.Name, options.ConnectionString, options.Tag, options.IsDefault)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error executing insert statement: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error retrieving inserted account: %w", err)
	}

	options.Id = int(id)
	return *options, nil
}

func (m *SQLiteManager) GetAccountByName(name string) (AccountOptions, error) {
	if m.db == nil {
		return AccountOptions{}, fmt.Errorf("database connection is not initialized")
	}

	var totalAccounts int
	err := m.db.QueryRow("SELECT COUNT(*) FROM account WHERE name = ?", name).Scan(&totalAccounts)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error checking account count: %w", err)
	}

	if totalAccounts == 0 {
		return AccountOptions{}, fmt.Errorf("account with name '%s' not found", name)
	}

	queryStatement, err := m.db.Prepare("SELECT id, name, connectionString, tag, isDefault FROM account WHERE name = ?")
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error preparing query statement: %w", err)
	}
	defer queryStatement.Close()

	var account AccountOptions
	err = queryStatement.QueryRow(name).Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag, &account.IsDefault)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error executing query statement: %w", err)
	}

	return account, nil
}

func (m *SQLiteManager) GetAccounts() ([]AccountOptions, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	var accounts []AccountOptions
	row, err := m.db.Query("SELECT a.id, a.name, a.connectionString, a.tag, a.isDefault FROM account as a ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer row.Close()

	for row.Next() {
		var account AccountOptions
		err := row.Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag, &account.IsDefault)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err := row.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

func (m *SQLiteManager) DeleteAccountByName(name string) error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	statement, err := m.db.Prepare("DELETE FROM account WHERE name = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer statement.Close()

	result, err := statement.Exec(name)
	if err != nil {
		return fmt.Errorf("failed to execute delete statement: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no account with name '%s' found", name)
	}

	return nil
}

func (m *SQLiteManager) UpdateDefaultItem(name string) error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	resetStmt, err := m.db.Prepare("UPDATE account SET isDefault = 0")
	if err != nil {
		return err
	}
	_, err = resetStmt.Exec()
	if err != nil {
		return err
	}

	updateStmt, err := m.db.Prepare("UPDATE account SET isDefault = 1 WHERE name = ?")
	if err != nil {
		return err
	}
	_, err = updateStmt.Exec(name)
	if err != nil {
		return err
	}

	return nil
}

type SavedQueryOptions struct {
	Id           int
	Name         string
	QueryString  string
	DatabaseId   string
	ContainerId  string
	AccountName  string
	Description  string
	DateCreated  string
	DateModified string
}

func (m *SQLiteManager) CreateSavedQueryTable() error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS saved_query (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"queryString" TEXT,
		"databaseId" TEXT,
		"containerId" TEXT,
		"accountName" TEXT,
		"description" TEXT,
		"dateCreated" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		"dateModified" TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	  );`

	statement, err := m.db.Prepare(createTableSQL)
	if err != nil {
		return err
	}

	_, err = statement.Exec()
	if err != nil {
		return err
	}

	return nil
}

func (m *SQLiteManager) EnsureSavedQueryTableExists() error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	var tableName string
	err := m.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='saved_query'").Scan(&tableName)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return m.CreateSavedQueryTable()
		}
		return err
	}

	return nil
}

func (m *SQLiteManager) SaveQuery(options *SavedQueryOptions) (SavedQueryOptions, error) {
	if m.db == nil {
		return SavedQueryOptions{}, fmt.Errorf("database connection is not initialized")
	}

	// Check if query with this name already exists
	_, err := m.GetSavedQueryByName(options.Name)
	if err == nil {
		// Query exists, update it
		updateQuerySQL := `UPDATE saved_query SET
			queryString = ?,
			databaseId = ?,
			containerId = ?,
			accountName = ?,
			description = ?,
			dateModified = CURRENT_TIMESTAMP
			WHERE name = ?`

		statement, err := m.db.Prepare(updateQuerySQL)
		if err != nil {
			return SavedQueryOptions{}, fmt.Errorf("error preparing update statement: %w", err)
		}
		defer statement.Close()

		_, err = statement.Exec(
			options.QueryString,
			options.DatabaseId,
			options.ContainerId,
			options.AccountName,
			options.Description,
			options.Name,
		)
		if err != nil {
			return SavedQueryOptions{}, fmt.Errorf("error executing update statement: %w", err)
		}

		log.Println("Updated saved query successfully")
	} else {
		insertQuerySQL := `INSERT INTO saved_query(
			name,
			queryString,
			databaseId,
			containerId,
			accountName,
			description)
			VALUES (?, ?, ?, ?, ?, ?)`

		statement, err := m.db.Prepare(insertQuerySQL)
		if err != nil {
			return SavedQueryOptions{}, fmt.Errorf("error preparing insert statement: %w", err)
		}
		defer statement.Close()

		_, err = statement.Exec(
			options.Name,
			options.QueryString,
			options.DatabaseId,
			options.ContainerId,
			options.AccountName,
			options.Description,
		)
		if err != nil {
			return SavedQueryOptions{}, fmt.Errorf("error executing insert statement: %w", err)
		}

		log.Println("Saved query successfully")
	}

	return m.GetSavedQueryByName(options.Name)
}

func (m *SQLiteManager) GetSavedQueryByName(name string) (SavedQueryOptions, error) {
	if m.db == nil {
		return SavedQueryOptions{}, fmt.Errorf("database connection is not initialized")
	}

	queryStatement, err := m.db.Prepare(`
		SELECT id, name, queryString, databaseId, containerId, accountName, description,
		datetime(dateCreated, 'localtime'), datetime(dateModified, 'localtime')
		FROM saved_query WHERE name = ?`)
	if err != nil {
		return SavedQueryOptions{}, fmt.Errorf("error preparing query statement: %w", err)
	}
	defer queryStatement.Close()

	var savedQuery SavedQueryOptions
	err = queryStatement.QueryRow(name).Scan(
		&savedQuery.Id,
		&savedQuery.Name,
		&savedQuery.QueryString,
		&savedQuery.DatabaseId,
		&savedQuery.ContainerId,
		&savedQuery.AccountName,
		&savedQuery.Description,
		&savedQuery.DateCreated,
		&savedQuery.DateModified,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SavedQueryOptions{}, fmt.Errorf("saved query with name '%s' not found", name)
		}
		return SavedQueryOptions{}, fmt.Errorf("error querying saved query: %w", err)
	}

	return savedQuery, nil
}

func (m *SQLiteManager) GetSavedQueries() ([]SavedQueryOptions, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	var savedQueries []SavedQueryOptions
	rows, err := m.db.Query(`
		SELECT id, name, queryString, databaseId, containerId, accountName, description,
		datetime(dateCreated, 'localtime'), datetime(dateModified, 'localtime')
		FROM saved_query ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("error querying saved queries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var savedQuery SavedQueryOptions
		err := rows.Scan(
			&savedQuery.Id,
			&savedQuery.Name,
			&savedQuery.QueryString,
			&savedQuery.DatabaseId,
			&savedQuery.ContainerId,
			&savedQuery.AccountName,
			&savedQuery.Description,
			&savedQuery.DateCreated,
			&savedQuery.DateModified,
		)
		if err != nil {
			return nil, fmt.Errorf("error scanning saved query row: %w", err)
		}
		savedQueries = append(savedQueries, savedQuery)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating saved query rows: %w", err)
	}

	return savedQueries, nil
}

func (m *SQLiteManager) DeleteSavedQueryByName(name string) error {
	if m.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	statement, err := m.db.Prepare("DELETE FROM saved_query WHERE name = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer statement.Close()

	result, err := statement.Exec(name)
	if err != nil {
		return fmt.Errorf("failed to execute delete statement: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no saved query with name '%s' found", name)
	}

	return nil
}
