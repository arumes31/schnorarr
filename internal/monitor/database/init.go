package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"strconv"
	"strings"
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
			_ = DB.Close()
			continue
		}

		if _, err = DB.Exec("PRAGMA busy_timeout=5000"); err != nil {
			_ = DB.Close()
			continue
		}

		if err := runMigrations(); err != nil {
			_ = DB.Close()
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

	for _, file := range files {
		// Parse version from filename (e.g., "001_init.sql" -> 1)
		parts := strings.SplitN(file.Name(), "_", 2)
		if len(parts) < 2 {
			log.Printf("[Database] Skipping invalid migration file: %s", file.Name())
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			log.Printf("[Database] Skipping invalid migration version: %s", file.Name())
			continue
		}

		// Check if migration already run
		var exists int
		_ = DB.QueryRow("SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&exists)
		if exists == 1 {
			continue
		}

		log.Printf("[Database] Running migration %d: %s", version, file.Name())
		content, err := migrationFS.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}

		// Execute in transaction
		tx, err := DB.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			// If it's an "already exists" error on create table, we might want to ignore it if we are sure,
			// but for safety we fail. The user can manually fix if needed.
			return fmt.Errorf("migration %s failed: %w", file.Name(), err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration version: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration: %w", err)
		}
	}

	return nil
}
