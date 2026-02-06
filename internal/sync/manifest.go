package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

// FileInfo represents metadata about a single file
type FileInfo struct {
	Path    string    // Relative path from sync root
	Size    int64     // File size in bytes
	ModTime time.Time // Last modification time
	Hash    string    // SHA256 hash (computed on demand)
	IsDir   bool      // True if this is a directory
}

// Manifest represents the complete file tree of a sync location
type Manifest struct {
	Root  string               // Absolute path to sync root
	Files map[string]*FileInfo // Map of relative path -> FileInfo
	Dirs  map[string]bool      // Set of directories (for quick lookup)
}

// NewManifest creates an empty manifest for the given root path
func NewManifest(root string) *Manifest {
	return &Manifest{
		Root:  root,
		Files: make(map[string]*FileInfo),
		Dirs:  make(map[string]bool),
	}
}

// AddFile adds a file to the manifest
func (m *Manifest) Add(info *FileInfo) {
	m.Files[info.Path] = info
	if info.IsDir {
		m.Dirs[info.Path] = true
	}
}

// HasFile checks if a file exists in the manifest
func (m *Manifest) HasFile(path string) bool {
	_, exists := m.Files[path]
	return exists
}

// HasDir checks if a directory exists in the manifest
func (m *Manifest) HasDir(path string) bool {
	return m.Dirs[path]
}

// ComputeHash calculates the SHA256 hash of a file
func (fi *FileInfo) ComputeHash(fullPath string) error {
	if fi.IsDir {
		return nil // Directories don't have hashes
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}

	fi.Hash = hex.EncodeToString(hash.Sum(nil))
	return nil
}

// NeedsUpdate determines if a file should be updated based on size/mtime comparison
func (fi *FileInfo) NeedsUpdate(other *FileInfo) bool {
	if fi.IsDir != other.IsDir {
		return true
	}

	if fi.IsDir {
		return false // Directories don't need updates
	}

	// File needs update if size differs or sender is newer
	return fi.Size != other.Size || fi.ModTime.After(other.ModTime)
}
