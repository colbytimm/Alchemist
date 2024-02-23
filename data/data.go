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
		"name" TEXT,
		"connectionString" TEXT,
		"tag" TEXT
	  );`

	statement, err := db.Prepare(createTableSQL)
	if err != nil {
		log.Fatal(err.Error())
	}

	statement.Exec()
	log.Println("Alchemist account table created")
}

func InsertAccount(options *AccountOptions) {
	insertAccountSQL := `INSERT INTO account(name, connectionString, tag) VALUES (?, ?, ?)`
	statement, err := db.Prepare(insertAccountSQL)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = statement.Exec(options.Name, options.ConnectionString, options.Tag)
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("Inserted account successfully")
}

func GetAccounts() []AccountOptions {
	var accounts []AccountOptions
	row, err := db.Query("SELECT a.id, a.name, a.connectionString, a.tag FROM account as a ORDER BY name")
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()

	for row.Next() {
		var account AccountOptions
		row.Scan(&account.Id, &account.Name, &account.ConnectionString, &account.Tag)
		accounts = append(accounts, account)
	}
	return accounts
}

func DeleteAccountById(id int) error {
	statement, err := db.Prepare("DELETE FROM items WHERE id = ?")
	if err != nil {
		log.Fatalf("prepare delete statement error: %v", err)
	}
	defer statement.Close()

	_, err = statement.Exec(id)
	if err != nil {
		log.Fatalf("executing delete error: %v", err)
	}

	log.Println("Deleted account successfully")
	return nil
}
