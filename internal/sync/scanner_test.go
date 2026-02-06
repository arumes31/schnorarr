package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanner_ScanLocal(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"file1.txt",
		"dir1/file2.txt",
		"dir1/dir2/file3.txt",
		"dir3/file4.txt",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tmpDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Scan directory
	scanner := NewScanner()
	manifest, err := scanner.ScanLocal(tmpDir)
	if err != nil {
		t.Fatalf("Failed to scan: %v", err)
	}

	// Verify files
	expectedFiles := 4
	actualFiles := 0
	for _, info := range manifest.Files {
		if !info.IsDir {
			actualFiles++
		}
	}

	if actualFiles != expectedFiles {
		t.Errorf("Expected %d files, got %d", expectedFiles, actualFiles)
	}

	// Verify directories
	if !manifest.HasDir("dir1") {
		t.Error("dir1 should exist")
	}
	if !manifest.HasDir("dir1/dir2") {
		t.Error("dir1/dir2 should exist")
	}

	// Verify file details
	file1 := manifest.Files["file1.txt"]
	if file1 == nil {
		t.Fatal("file1.txt not found in manifest")
	}
	if file1.Size != 4 { // "test" = 4 bytes
		t.Errorf("Expected size 4, got %d", file1.Size)
	}
}

func TestScanner_Exclusions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files including ones that should be excluded
	testFiles := []string{
		"file1.txt",
		".git/config",
		"dir/.DS_Store",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tmpDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	scanner := NewScanner()
	manifest, err := scanner.ScanLocal(tmpDir)
	if err != nil {
		t.Fatalf("Failed to scan: %v", err)
	}

	// Should only have file1.txt
	if manifest.HasFile(".git/config") {
		t.Error(".git/config should be excluded")
	}
	if manifest.HasFile("dir/.DS_Store") {
		t.Error(".DS_Store should be excluded")
	}
	if !manifest.HasFile("file1.txt") {
		t.Error("file1.txt should be included")
	}
}
