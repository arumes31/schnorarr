package database

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) {
	var err error
	// Use distinct in-memory DB for each test run if possible, but global DB var makes it hard.
	// Since tests run sequentially in this package, overwriting DB is acceptable.
	DB, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}

	// Recreate schema manually for testing
	_, err = DB.Exec(`CREATE TABLE IF NOT EXISTS history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT,
    action TEXT,
    file_path TEXT,
    size_bytes INTEGER DEFAULT 0,
    engine_id TEXT DEFAULT ''
	);`)
	if err != nil {
		t.Fatalf("Failed to create history table: %v", err)
	}
}

func TestLogEvent(t *testing.T) {
	setupTestDB(t)
	defer func() { _ = DB.Close() }()

	err := LogEvent("2023-01-01 10:00:00", "Sync", "/path/to/file", 1234, "engine1")
	if err != nil {
		t.Errorf("LogEvent failed: %v", err)
	}

	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM history").Scan(&count)
	if err != nil {
		t.Errorf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 row, got %d", count)
	}

	var engineID string
	err = DB.QueryRow("SELECT engine_id FROM history LIMIT 1").Scan(&engineID)
	if err != nil {
		t.Fatal(err)
	}
	if engineID != "engine1" {
		t.Errorf("Expected engineID 'engine1', got '%s'", engineID)
	}
}

func TestPruneHistory(t *testing.T) {
	setupTestDB(t)
	defer func() { _ = DB.Close() }()

	// Insert old record (10 days ago)
	_, err := DB.Exec("INSERT INTO history (timestamp, action, file_path) VALUES (date('now', '-10 days'), 'Old', '/old/file')")
	if err != nil {
		t.Fatal(err)
	}
	// Insert new record (1 day ago)
	_, err = DB.Exec("INSERT INTO history (timestamp, action, file_path) VALUES (date('now', '-1 day'), 'New', '/new/file')")
	if err != nil {
		t.Fatal(err)
	}

	// Prune older than 5 days
	err = PruneHistory(5)
	if err != nil {
		t.Errorf("PruneHistory failed: %v", err)
	}

	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM history").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("Expected 1 row remaining, got %d", count)
	}

	var action string
	err = DB.QueryRow("SELECT action FROM history LIMIT 1").Scan(&action)
	if err != nil {
		t.Fatal(err)
	}
	if action != "New" {
		t.Errorf("Expected remaining row to be 'New', got '%s'", action)
	}
}
