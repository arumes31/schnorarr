package database

import (
	"os"
	"testing"
)

func TestGetDBPath(t *testing.T) {
	// 1. Test environment override
	os.Setenv("DB_PATH", "test.db")
	defer os.Unsetenv("DB_PATH")

	path := getDBPath()
	if path != "test.db" {
		t.Errorf("Expected test.db, got %s", path)
	}

	// 2. Test default (Linux-like)
	os.Unsetenv("DB_PATH")
	// We can't easily mock os.PathSeparator, but we can check the behavior
	path = getDBPath()
	// On Windows without /config, it should be history.db
	if os.PathSeparator == '\\' {
		if _, err := os.Stat("C:\\config"); err != nil {
			if path != "history.db" {
				t.Errorf("Expected history.db on Windows without C:\\config, got %s", path)
			}
		}
	} else {
		if path != defaultDBPath {
			t.Errorf("Expected %s on Linux, got %s", defaultDBPath, path)
		}
	}
}

func TestDBInit(t *testing.T) {
	os.Setenv("DB_PATH", "test_init.db")
	defer os.Remove("test_init.db")
	defer os.Unsetenv("DB_PATH")

	DBPath = getDBPath()
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer DB.Close()

	// Verify table exists
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM history").Scan(&count)
	if err != nil {
		t.Errorf("history table should exist: %v", err)
	}
}
