package data

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type AccountOptions struct {
	Id               int
	Name             string
	ConnectionString string
	Tag              string
	IsDefault        bool
}

func OpenDatabase() error {
	var err error

	db, err = sql.Open("sqlite3", "./alchemist-database.db")
	if err != nil {
		return err
	}

	return db.Ping()
}

func CreateAccountTable() error {
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

// EnsureAccountTableExists checks if the account table exists and creates it if it doesn't
func EnsureAccountTableExists() error {
	// Check if the account table exists
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='account'").Scan(&tableName)

	if err != nil {
		// If the error is "no rows in result set", the table doesn't exist
		if err.Error() == "sql: no rows in result set" {
			// Create the table
			return CreateAccountTable()
		}
		return err
	}

	// Table exists
	return nil
}

func InsertAccount(options *AccountOptions) AccountOptions {
	insertAccountSQL := `INSERT INTO account(name, connectionString, tag, isDefault) VALUES (?, ?, ?, ?)`
	statement, err := db.Prepare(insertAccountSQL)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = statement.Exec(options.Name, options.ConnectionString, options.Tag, 0)
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("Inserted account successfully")

	account := GetAccountByName(options.Name)
	return account
}

func GetAccountByName(name string) AccountOptions {
	queryStatement, err := db.Prepare("SELECT a.id, a.name, a.connectionString, a.tag, a.isDefault FROM account as a WHERE name = ?")
	if err != nil {
		log.Fatal(err)
	}
	defer queryStatement.Close()

	row, err := queryStatement.Query(name)
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()

	for row.Next() {
		var account AccountOptions
		row.Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag)
		return account
	}
	if err := row.Err(); err != nil {
		log.Fatal(err)
	}

	return AccountOptions{}
}

func GetAccounts() ([]AccountOptions, error) {
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
	statement, err := db.Prepare("DELETE FROM account WHERE name = ?")
	if err != nil {
		log.Fatalf("prepare delete statement error: %v", err)
	}
	defer statement.Close()

	_, err = statement.Exec(name)
	if err != nil {
		log.Fatalf("executing delete error: %v", err)
	}

	log.Println("Deleted account successfully")
	return nil
}

func UpdateDefaultItem(name string) error {
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
