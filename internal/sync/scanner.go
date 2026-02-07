package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// ScanLocal scans a local directory and builds a manifest using parallel workers
func (s *Scanner) ScanLocal(root string) (*Manifest, error) {
	manifest := NewManifest(root)
	log.Printf("[Scanner] Starting parallel scan of %s", root)

	// Mutex for manifest map writes
	var mu sync.Mutex
	
	// Semaphore to limit concurrency (prevent opening too many file descriptors)
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup

	// Error channel
	errCh := make(chan error, 1)

	// Helper to process a directory
	var walkDir func(string)
	walkDir = func(dir string) {
		defer wg.Done()

		// Acquire semaphore
		sem <- struct{}{}
		defer func() { <-sem }()

		entries, err := os.ReadDir(dir)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("failed to read dir %s: %w", dir, err):
			default:
			}
			return
		}

		for _, d := range entries {
			// Check for abort
			select {
			case <-errCh:
				return
			default:
			}

			fullPath := filepath.Join(dir, d.Name())
			relPath, err := filepath.Rel(root, fullPath)
			if err != nil {
				continue
			}

			// Check exclusions
			if s.shouldExclude(relPath) {
				if d.IsDir() {
					log.Printf("[Scanner] Skipping excluded directory: %s", relPath)
				}
				continue
			}

			// Get info
			info, err := d.Info()
			if err != nil {
				continue
			}

			fileInfo := &FileInfo{
				Path:    filepath.ToSlash(relPath),
				Size:    info.Size(),
				ModTime: info.ModTime(),
				IsDir:   d.IsDir(),
			}

			if s.ComputeHashes && !d.IsDir() {
				if err := fileInfo.ComputeHash(fullPath); err != nil {
					log.Printf("[Scanner] Hash error for %s: %v", fullPath, err)
				}
			}

			mu.Lock()
			manifest.Add(fileInfo)
			mu.Unlock()

			if d.IsDir() {
				wg.Add(1)
				go walkDir(fullPath)
			}
		}
	}

	// Start root walk
	wg.Add(1)
	go walkDir(root)

	// Wait for completion or error
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case err := <-errCh:
		return nil, err
	}

	log.Printf("[Scanner] Finished scan of %s: found %d items", root, len(manifest.Files)+len(manifest.Dirs))
	return manifest, nil
}

// shouldExclude checks if a path matches any exclusion pattern
func (s *Scanner) shouldExclude(path string) bool {
	for _, pattern := range s.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}
	return false
}