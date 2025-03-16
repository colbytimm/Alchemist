package data

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB
var inTestMode bool = false
var mockActive bool = false

// TODO: Improve testing for this file
// Mock function variables that can be set in tests
var (
	OpenDatabaseMock             func() error
	EnsureAccountTableExistsMock func() error
	InsertAccountMock            func(*AccountOptions) (AccountOptions, error)
	GetAccountByNameMock         func(string) (AccountOptions, error)
	GetAccountsMock              func() ([]AccountOptions, error)
	DeleteAccountByNameMock      func(string) error
	UpdateDefaultItemMock        func(string) error
)

// GetDB returns the current database connection for testing purposes
func GetDB() *sql.DB {
	return db
}

// SetDB sets the database connection for testing purposes
func SetDB(database *sql.DB) {
	db = database

	if database == nil {
		inTestMode = true
	} else {
		inTestMode = false
	}
}

func ActivateMock() {
	mockActive = true
}

func DeactivateMock() {
	mockActive = false
	// Reset all mock functions
	OpenDatabaseMock = nil
	EnsureAccountTableExistsMock = nil
	InsertAccountMock = nil
	GetAccountByNameMock = nil
	GetAccountsMock = nil
	DeleteAccountByNameMock = nil
	UpdateDefaultItemMock = nil
}

type AccountOptions struct {
	Id               int
	Name             string
	ConnectionString string
	Tag              string
	IsDefault        bool
}

func OpenDatabase() error {
	if mockActive && OpenDatabaseMock != nil {
		return OpenDatabaseMock()
	}

	var err error

	if db != nil {
		return nil
	}

	if inTestMode {
		return fmt.Errorf("database connection is not initialized")
	}

	db, err = sql.Open("sqlite3", "./alchemist-database.db")
	if err != nil {
		return err
	}

	return db.Ping()
}

func CreateAccountTable() error {
	if db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS account (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"connectionString" TEXT,
		"tag" TEXT,
		"isDefault" BOOLEAN
	  );`

	statement, err := db.Prepare(createTableSQL)
	if err != nil {
		return err
	}

	_, err = statement.Exec()
	if err != nil {
		return err
	}

	return nil
}

func EnsureAccountTableExists() error {
	if mockActive && EnsureAccountTableExistsMock != nil {
		return EnsureAccountTableExistsMock()
	}

	if mockActive {
		return nil
	}

	if db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='account'").Scan(&tableName)

	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return CreateAccountTable()
		}
		return err
	}

	return nil
}

func InsertAccount(options *AccountOptions) (AccountOptions, error) {
	if mockActive && InsertAccountMock != nil {
		return InsertAccountMock(options)
	}

	if mockActive {
		return AccountOptions{
			Id:               1,
			Name:             options.Name,
			ConnectionString: options.ConnectionString,
			Tag:              options.Tag,
			IsDefault:        true,
		}, nil
	}

	if db == nil {
		return AccountOptions{}, fmt.Errorf("database connection is not initialized")
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM account").Scan(&count)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error checking account count: %v", err)
	}

	isDefault := 0
	if count == 0 {
		isDefault = 1
	}

	insertAccountSQL := `INSERT INTO account(name, connectionString, tag, isDefault) VALUES (?, ?, ?, ?)`
	statement, err := db.Prepare(insertAccountSQL)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error preparing insert statement: %v", err)
	}
	defer statement.Close()

	_, err = statement.Exec(options.Name, options.ConnectionString, options.Tag, isDefault)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error executing insert statement: %v", err)
	}

	log.Println("Inserted account successfully")

	account, err := GetAccountByName(options.Name)
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error retrieving inserted account: %v", err)
	}
	return account, nil
}

func GetAccountByName(name string) (AccountOptions, error) {
	if mockActive && GetAccountByNameMock != nil {
		return GetAccountByNameMock(name)
	}

	if mockActive {
		return AccountOptions{
			Id:               1,
			Name:             name,
			ConnectionString: "mock-connection-string",
			Tag:              "mock",
			IsDefault:        true,
		}, nil
	}

	if db == nil {
		return AccountOptions{}, fmt.Errorf("database connection is not initialized")
	}

	queryStatement, err := db.Prepare("SELECT a.id, a.name, a.connectionString, a.tag, a.isDefault FROM account as a WHERE name = ?")
	if err != nil {
		return AccountOptions{}, fmt.Errorf("error preparing query statement: %v", err)
	}
	defer queryStatement.Close()

	var account AccountOptions
	err = queryStatement.QueryRow(name).Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag, &account.IsDefault)
	if err != nil {
		if err == sql.ErrNoRows {
			return AccountOptions{}, fmt.Errorf("account with name '%s' not found", name)
		}
		return AccountOptions{}, fmt.Errorf("error querying account: %v", err)
	}

	return account, nil
}

func GetAccounts() ([]AccountOptions, error) {
	if mockActive && GetAccountsMock != nil {
		return GetAccountsMock()
	}

	if mockActive {
		return []AccountOptions{}, nil
	}

	if db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	var accounts []AccountOptions
	row, err := db.Query("SELECT a.id, a.name, a.connectionString, a.tag, a.isDefault FROM account as a ORDER BY id")
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

	if err = row.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

func DeleteAccountByName(name string) error {
	if mockActive && DeleteAccountByNameMock != nil {
		return DeleteAccountByNameMock(name)
	}

	if mockActive {
		return nil
	}

	if db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	statement, err := db.Prepare("DELETE FROM account WHERE name = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %v", err)
	}
	defer statement.Close()

	result, err := statement.Exec(name)
	if err != nil {
		return fmt.Errorf("failed to execute delete statement: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no account with name '%s' found", name)
	}

	return nil
}

func UpdateDefaultItem(name string) error {
	if mockActive && UpdateDefaultItemMock != nil {
		return UpdateDefaultItemMock(name)
	}

	if mockActive {
		return nil
	}

	if db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	resetStmt, err := db.Prepare("UPDATE account SET isDefault = 0")
	if err != nil {
		return err
	}
	_, err = resetStmt.Exec()
	if err != nil {
		return err
	}

	updateStmt, err := db.Prepare("UPDATE account SET isDefault = 1 WHERE name = ?")
	if err != nil {
		return err
	}
	_, err = updateStmt.Exec(name)
	if err != nil {
		return err
	}

	return nil
}

func IsInTestMode() bool {
	return inTestMode
}
