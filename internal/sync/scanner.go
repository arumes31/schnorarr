package sync

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Scanner handles directory traversal and manifest building
type Scanner struct {
	// ExcludePatterns defines glob patterns to exclude from scanning
	ExcludePatterns []string
	// ComputeHashes enables hash computation (slower but more accurate)
	ComputeHashes bool
}

// NewScanner creates a new scanner with default settings
func NewScanner() *Scanner {
	return &Scanner{
		ExcludePatterns: []string{
			".git",
			".DS_Store",
			"Thumbs.db",
		},
		ComputeHashes: false, // Use mtime by default for performance
	}
}

// ScanLocal scans a local directory and builds a manifest
func (s *Scanner) ScanLocal(root string) (*Manifest, error) {
	manifest := NewManifest(root)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from root
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Skip the root itself
		if relPath == "." {
			return nil
		}

		// Check exclusions
		if s.shouldExclude(relPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		// Create FileInfo
		fileInfo := &FileInfo{
			Path:    filepath.ToSlash(relPath), // Normalize to forward slashes
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   d.IsDir(),
		}

		// Compute hash if enabled and not a directory
		if s.ComputeHashes && !d.IsDir() {
			if err := fileInfo.ComputeHash(path); err != nil {
				return fmt.Errorf("failed to compute hash for %s: %w", path, err)
			}
		}

		manifest.Add(fileInfo)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", root, err)
	}

	return manifest, nil
}

// shouldExclude checks if a path matches any exclusion pattern
func (s *Scanner) shouldExclude(path string) bool {
	for _, pattern := range s.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
		// Also check if any path component matches
		parts := strings.Split(filepath.ToSlash(path), "/")
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}
	return false
}
