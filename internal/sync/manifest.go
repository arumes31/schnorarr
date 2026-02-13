package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// FileInfo represents metadata about a single file
type FileInfo struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Hash    string    `json:"hash,omitempty"`
	IsDir   bool      `json:"isDir"`
}

// Manifest represents the complete file tree of a sync location
type Manifest struct {
	Root  string               `json:"root"`
	Files map[string]*FileInfo `json:"files"`
	Dirs  map[string]bool      `json:"dirs"`

	// Non-exported case-insensitive index
	lowerFiles map[string]string
	lowerDirs  map[string]string
	mu         sync.RWMutex
}

// NewManifest creates an empty manifest for the given root path
func NewManifest(root string) *Manifest {
	return &Manifest{
		Root:  root,
		Files: make(map[string]*FileInfo),
		Dirs:  make(map[string]bool),
	}
}

// Add adds a file or directory to the manifest
func (m *Manifest) Add(info *FileInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Files[info.Path] = info
	if info.IsDir {
		m.Dirs[info.Path] = true
	}
	// Invalidate case-insensitive index
	m.lowerFiles = nil
	m.lowerDirs = nil
}

// HasFile checks if a file exists in the manifest (exact match)
func (m *Manifest) HasFile(path string) bool {
	m.mu.RLock()
	_, exists := m.Files[path]
	m.mu.RUnlock()
	return exists
}

// HasDir checks if a directory exists in the manifest (exact match)
func (m *Manifest) HasDir(path string) bool {
	m.mu.RLock()
	exists := m.Dirs[path]
	m.mu.RUnlock()
	return exists
}

// GetFile retrieves a file from the manifest, trying exact match first,
// then case-insensitive match.
func (m *Manifest) GetFile(path string) (*FileInfo, bool) {
	m.mu.RLock()
	if f, ok := m.Files[path]; ok {
		m.mu.RUnlock()
		return f, true
	}
	m.mu.RUnlock()

	// Try case-insensitive
	m.ensureIndexes()
	m.mu.RLock()
	defer m.mu.RUnlock()

	if realPath, ok := m.lowerFiles[strings.ToLower(path)]; ok {
		return m.Files[realPath], true
	}
	return nil, false
}

// GetDir checks if a directory exists in the manifest, trying exact match first,
// then case-insensitive match. Returns the real path if found.
func (m *Manifest) GetDir(path string) (string, bool) {
	m.mu.RLock()
	if m.Dirs[path] {
		m.mu.RUnlock()
		return path, true
	}
	m.mu.RUnlock()

	// Try case-insensitive
	m.ensureIndexes()
	m.mu.RLock()
	defer m.mu.RUnlock()

	if realPath, ok := m.lowerDirs[strings.ToLower(path)]; ok {
		return realPath, true
	}
	return "", false
}

func (m *Manifest) ensureIndexes() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.lowerFiles != nil {
		return
	}

	m.lowerFiles = make(map[string]string)
	for p := range m.Files {
		m.lowerFiles[strings.ToLower(p)] = p
	}

	m.lowerDirs = make(map[string]string)
	for p := range m.Dirs {
		m.lowerDirs[strings.ToLower(p)] = p
	}
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
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}

	fi.Hash = hex.EncodeToString(hash.Sum(nil))
	return nil
}

// NeedsUpdate determines if a file should be updated based on size/mtime comparison.
// It uses a 1-second threshold to ignore sub-second differences.
func (fi *FileInfo) NeedsUpdate(other *FileInfo) bool {
	if fi.IsDir != other.IsDir {
		return true
	}

	if fi.IsDir {
		return false // Directories don't need updates
	}

	if fi.Size != other.Size {
		return true
	}

	// Truncate to seconds for comparison to avoid precision mismatches
	return fi.ModTime.Unix() > other.ModTime.Unix()
}

// GetFileCountInDir counts how many files (not directories) are directly inside the given directory path.
// It performs a linear scan which is O(N) where N is total files in manifest.
// For frequent usage, an index would be better, but for deletion checks it's acceptable.
func (m *Manifest) GetFileCountInDir(dirPath string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	cleanDir := strings.TrimSuffix(dirPath, "/")

	for path, info := range m.Files {
		if info.IsDir {
			continue
		}
		// check if file's parent is the target directory
		parent := strings.TrimSuffix(path, "/")
		idx := strings.LastIndex(parent, "/")
		if idx != -1 {
			parent = parent[:idx]
		} else {
			parent = ""
		}

		if parent == cleanDir {
			count++
		}
	}
	return count
}
