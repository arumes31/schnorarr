package sync

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestTransferer_CopyParallel(t *testing.T) {

	// Create a dummy file (1MB)
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.dat")
	dstPath := filepath.Join(tmpDir, "dst.dat")

	size := int64(1 * 1024 * 1024)
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	// We close explicitly after copy to flush, but defer just in case
	defer dstFile.Close()

	tr := NewTransferer(TransferOptions{})

	// Direct call to private method to verify logic regardless of Threshold
	written, err := tr.copyParallel("test.dat", srcFile, dstFile, size)
	if err != nil {
		t.Fatalf("copyParallel failed: %v", err)
	}
	if written != size {
		t.Errorf("Expected %d bytes written, got %d", size, written)
	}

	// Sync and Close to ensure flush
	if err := dstFile.Sync(); err != nil {
		t.Fatal(err)
	}
	dstFile.Close()

	// Verify content
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, dstData) {
		t.Error("Destination content mismatch")
	}
}

// Todo: Test CopyFile retry logic
// This requires mocking os.Open/Create or filesystem fault injection, which is complex.
// For now, we rely on the manual verification of the seek reset fix.
