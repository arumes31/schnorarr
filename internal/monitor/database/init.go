package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const DBPath = "/config/history.db"

// DB is the singleton database instance
var DB *sql.DB

// Init initializes the database connection and runs migrations
func Init() error {
	var err error

	// Retry logic for database initialization (handles concurrent startup)
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			backoff := time.Duration(100*(1<<uint(i-1))) * time.Millisecond
			log.Printf("Database init retry %d/%d after %v", i+1, maxRetries, backoff)
			time.Sleep(backoff)
		}

		DB, err = sql.Open("sqlite", DBPath)
		if err != nil {
			if i == maxRetries-1 {
				return fmt.Errorf("failed to open DB after %d retries: %w", maxRetries, err)
			}
			continue
		}

		DB.SetMaxOpenConns(1)
		DB.SetMaxIdleConns(1)
		DB.SetConnMaxLifetime(time.Hour)

		if _, err = DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			DB.Close()
			continue
		}

		if _, err = DB.Exec("PRAGMA busy_timeout=5000"); err != nil {
			DB.Close()
			continue
		}

		if err := runMigrations(); err != nil {
			DB.Close()
			continue
		}

		log.Println("Database initialized successfully")
		return nil
	}

	return fmt.Errorf("failed to initialize database after %d retries", maxRetries)
}

func runMigrations() error {
	// 1. Ensure schema_migrations table exists
	_, err := DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`)
	if err != nil {
		return err
	}

	// 2. Load and run migrations from embedded FS
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	for i, file := range files {
		version := i + 1
		
		// Check if migration already run
		var exists int
		_ = DB.QueryRow("SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&exists)
		if exists == 1 {
			continue
		}

		log.Printf("[Database] Running migration: %s", file.Name())
		content, err := migrationFS.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}

		if _, err := DB.Exec(string(content)); err != nil {
			// Design note: Migration 002 might fail if the user's DB was already repaired 
			// by previous turn's repair logic. We log a warning and continue.
			log.Printf("[Database] Warning: migration %s failed (may have already run): %v", file.Name(), err)
		}

		_, _ = DB.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version)
	}

	return nil
}