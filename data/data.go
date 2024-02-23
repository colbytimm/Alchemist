package data

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

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
		"connectionString" TEXT,
		"name" TEXT,
		"tag" TEXT
	  );`

	statement, err := db.Prepare(createTableSQL)
	if err != nil {
		log.Fatal(err.Error())
	}

	statement.Exec()
	log.Println("Alchemist account table created")
}

func InsertAccount(connectionString string, name string, tag string) {
	insertAccountSQL := `INSERT INTO account(connectionString, name, tag) VALUES (?, ?, ?)`
	statement, err := db.Prepare(insertAccountSQL)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = statement.Exec(connectionString, name, tag)
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("Inserted account successfully")
}

func GetAccounts() {
	row, err := db.Query("SELECT * FROM account ORDER BY name")
	if err != nil {
		log.Fatal(err)
	}
	defer row.Close()

	for row.Next() {
		var id int
		var connectionString string
		var name string
		var tag string
		row.Scan(&id, &connectionString, &name, &tag)
		log.Println("[", tag, "] ", connectionString, "—", name)
	}
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
