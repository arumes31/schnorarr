package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransfererRejectsPathsOutsideConfiguredRoots(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	transferer := NewTransferer(TransferOptions{SourceRoot: sourceRoot, TargetRoot: targetRoot})

	if err := transferer.CopyFile(outside, filepath.Join(targetRoot, "copied.txt")); err == nil {
		t.Fatal("CopyFile accepted a source outside SourceRoot")
	}
	if err := transferer.DeleteFile(outside); err == nil {
		t.Fatal("DeleteFile accepted a path outside TargetRoot")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep" {
		t.Fatalf("outside file changed: content=%q error=%v", got, err)
	}
}

func TestTransfererDoesNotFollowEscapingSourceSymlink(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sourceRoot, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	transferer := NewTransferer(TransferOptions{SourceRoot: sourceRoot, TargetRoot: targetRoot})
	if err := transferer.CopyFile(link, filepath.Join(targetRoot, "copied.txt")); err == nil {
		t.Fatal("CopyFile followed a symlink outside SourceRoot")
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "copied.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected destination created: %v", err)
	}
}
