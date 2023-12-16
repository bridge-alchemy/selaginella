package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/evm-layer2/selaginella/db/migrate"
)

var DB *sql.DB

// initializes the database connection
func InitDB(dbUser, dbPassword, dbName, dbHost string, dbPort int) {
	var err error

	// Build database connection string
	connString := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%d sslmode=disable", dbUser, dbPassword, dbName, dbHost, dbPort)

	// Open database connection
	DB, err = sql.Open("postgres", connString)
	if err != nil {
		log.Fatal("Error opening database connection:", err)
	}

	// Check database connection
	err = DB.Ping()
	if err != nil {
		log.Fatal("Error pinging database:", err)
	}

	fmt.Println("Database connection established")

	// Set parameters such as database connection pool
	DB.SetMaxOpenConns(10)
	DB.SetMaxIdleConns(5)

	// Perform database migration
	if err := migrate.MigrateDB(DB, dbName); err != nil {
		log.Fatal("Error running database migrations:", err)
	}
}

// closes the database connection
func CloseDB() {
	if DB != nil {
		err := DB.Close()
		if err != nil {
			log.Println("Error closing database connection:", err)
		} else {
			fmt.Println("Database connection closed")
		}
	}
}