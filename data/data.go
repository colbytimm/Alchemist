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

func CreateAccountTable() {
	createTableSQL := `CREATE TABLE IF NOT EXISTS account (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT UNIQUE,
		"connectionString" TEXT,
		"tag" TEXT,
		"isDefault" BOOLEAN
	  );`

	statement, err := db.Prepare(createTableSQL)
	if err != nil {
		log.Fatal(err.Error())
	}

	statement.Exec()
	log.Println("Alchemist account table created")
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

func GetAccounts() []AccountOptions {
	var accounts []AccountOptions
	row, err := db.Query("SELECT a.id, a.name, a.connectionString, a.tag, a.isDefault FROM account as a ORDER BY id")
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()

	for row.Next() {
		var account AccountOptions
		row.Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag, &account.IsDefault)
		accounts = append(accounts, account)
	}
	return accounts
}

func DeleteAccountByName(name string) error {
	statement, err := db.Prepare("DELETE FROM items WHERE name = ?")
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
	notDefaultStatement, err := db.Prepare("UPDATE account SET isDefault = 0")
	if err != nil {
		log.Fatalf("prepare account isDefault false for items error: %v", err)
	}
	defer notDefaultStatement.Close()

	isDefaultStatement, err := db.Prepare("UPDATE account SET isDefault = 1 WHERE name = ?")
	if err != nil {
		log.Fatalf("prepare account isDefault true for items error: %v", err)
	}
	defer isDefaultStatement.Close()

	_, err = notDefaultStatement.Exec()
	if err != nil {
		log.Fatalf("executing account isDefault false for items error: %v", err)
	}
	_, err = isDefaultStatement.Exec(name)
	if err != nil {
		log.Fatalf("executing account isDefault true error: %v", err)
	}

	// log.Println("isDefault account successfully updated")
	return nil
}
