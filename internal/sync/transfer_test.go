package sync

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransferer_CopyParallel(t *testing.T) {

	// Create a dummy file (1MB)
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.dat")
	dstPath := filepath.Join(tmpDir, "dst.dat")

	size := int64(1 * 1024 * 1024)
	data := make([]byte, int(size))
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
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	// We close explicitly after copy to flush, but defer just in case
	defer func() { _ = dstFile.Close() }()

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
	_ = dstFile.Close()

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

// TestTransferer_CopyParallelLowLimit verifies that with a low nonzero engine
// share, the aggregate rate of all parallel streams does not exceed the
// share. (A previous per-stream floor let numStreams x floor leak through.)
func TestTransferer_CopyParallelLowLimit(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.dat")
	dstPath := filepath.Join(tmpDir, "dst.dat")

	// 4 KB at a 1 KB/s engine share: 256 B/s per stream, ~4s total.
	// With the old per-stream floor each stream ran at 1 KB/s (~1s total).
	size := int64(4 * 1024)
	data := make([]byte, int(size))
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
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstFile.Close() }()

	tr := NewTransferer(TransferOptions{BandwidthLimit: MinShareBps})

	start := time.Now()
	written, err := tr.copyParallel("test.dat", srcFile, dstFile, size)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("copyParallel failed: %v", err)
	}
	if written != size {
		t.Errorf("Expected %d bytes written, got %d", size, written)
	}

	// Ideal duration is ~4s; allow generous scheduling margin but catch the
	// old floored behavior (~1s).
	if elapsed < 3*time.Second {
		t.Errorf("Aggregate rate exceeded engine share: %d bytes in %v", size, elapsed)
	}

	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, dstData) {
		t.Error("Destination content mismatch")
	}
}
