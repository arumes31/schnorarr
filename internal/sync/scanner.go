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
	// IncludePatterns defines glob patterns to include in scanning
	IncludePatterns []string
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
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	// Error handling with cancellation
	errCh := make(chan error, 1)
	done := make(chan struct{})
	var errOnce sync.Once

	// Helper to process a directory
	var walkDir func(string)
	walkDir = func(dir string) {
		defer wg.Done()

		// Check for cancellation
		select {
		case <-done:
			return
		default:
		}

		// Acquire semaphore
		select {
		case sem <- struct{}{}:
		case <-done:
			return
		}
		defer func() { <-sem }()

		entries, err := os.ReadDir(dir)
		if err != nil {
			errOnce.Do(func() {
				// Non-blocking send
				select {
				case errCh <- fmt.Errorf("failed to read dir %s: %w", dir, err):
				default:
				}
				close(done) // Signal cancellation
			})
			return
		}

		for _, d := range entries {
			// Check for cancellation
			select {
			case <-done:
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

			// Check inclusions (only for files)
			if !d.IsDir() && !s.shouldInclude(relPath) {
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

	// Wait for completion in background
	go func() {
		wg.Wait()
		// Only close errCh if not already cancelled/closed via error
		select {
		case <-done:
		default:
			close(errCh)
		}
	}()

	// Wait for first error or completion
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
		// If err is nil (closed channel), we are done success
	case <-done:
		// Cancelled (should have error in errCh)
		return nil, <-errCh
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

// shouldInclude checks if a path matches any inclusion pattern
// If IncludePatterns is empty, it returns true (include everything)
func (s *Scanner) shouldInclude(path string) bool {
	if len(s.IncludePatterns) == 0 {
		return true
	}
	base := filepath.Base(path)
	for _, pattern := range s.IncludePatterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}
