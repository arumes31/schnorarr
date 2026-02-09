package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDBPath(t *testing.T) {
	// 1. Test environment override
	t.Setenv("DB_PATH", "test.db")

	path := getDBPath()
	if path != "test.db" {
		t.Errorf("Expected test.db, got %s", path)
	}

	// 2. Test default (Linux-like)
	t.Setenv("DB_PATH", "")
	// We can't easily mock os.PathSeparator, but we can check the behavior
	path = getDBPath()
	// On Windows without /config, it should be history.db
	if os.PathSeparator == '\\' {
		if _, err := os.Stat("C:\\config"); err != nil {
			if path != "history.db" {
				t.Errorf("Expected history.db on Windows without C:\\config, got %s", path)
			}
		} else {
			expected := filepath.Join("C:\\config", "history.db")
			if path != expected {
				t.Errorf("Expected %s on Windows with C:\\config, got %s", expected, path)
			}
		}
	} else {
		if path != defaultDBPath {
			t.Errorf("Expected %s on Linux, got %s", defaultDBPath, path)
		}
	}
}

func TestDBInit(t *testing.T) {
	t.Setenv("DB_PATH", "test_init.db")
	t.Cleanup(func() {
		if err := os.Remove("test_init.db"); err != nil && !os.IsNotExist(err) {
			t.Errorf("Failed to remove test database: %v", err)
		}
	})

	DBPath = getDBPath()
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		if err := DB.Close(); err != nil {
			t.Errorf("Failed to close database: %v", err)
		}
	})

	// Verify table exists
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM history").Scan(&count)
	if err != nil {
		t.Errorf("history table should exist: %v", err)
	}
}
