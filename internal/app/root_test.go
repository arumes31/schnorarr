package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootedPathRejectsEscapeAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "important.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	for _, path := range []string{"../important.txt", filepath.Join(directory, "file"), "escape/important.txt", "."} {
		if _, err := rootedPath(root, path, false, false); err == nil {
			t.Fatalf("rootedPath(%q) accepted an unsafe path", path)
		}
	}
}
