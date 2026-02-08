package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// ScanLocal scans a local directory or remote rsync target
func (s *Scanner) ScanLocal(root string) (*Manifest, error) {
	if strings.Contains(root, "::") || strings.HasPrefix(root, "rsync://") {
		return s.ScanRemote(root)
	}
	manifest := NewManifest(root)
	log.Printf("[Scanner] Starting parallel scan of %s", root)

	// Mutex for manifest map writes
	var mu sync.Mutex

	// Worker pool for directory processing
	numWorkers := 8
	jobs := make(chan string, 10000)
	var wg sync.WaitGroup

	// Error handling with cancellation
	errCh := make(chan error, 1)
	done := make(chan struct{})
	var errOnce sync.Once

	worker := func() {
		for dir := range jobs {
			func() {
				defer wg.Done()

				// Check for cancellation
				select {
				case <-done:
					return
				default:
				}

				entries, err := os.ReadDir(dir)
				if err != nil {
					errOnce.Do(func() {
						select {
						case errCh <- fmt.Errorf("failed to read dir %s: %w", dir, err):
						default:
						}
						close(done) // Signal cancellation
					})
					return
				}

				for _, d := range entries {
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

					if s.shouldExclude(relPath) {
						continue
					}

					if !d.IsDir() && !s.shouldInclude(relPath) {
						continue
					}

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
						// Ensure we don't block on jobs channel if cancelled
						select {
						case jobs <- fullPath:
						case <-done:
							wg.Done()
						}
					}
				}
			}()
		}
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		go worker()
	}

	// Initial job
	wg.Add(1)
	jobs <- root

	// Wait for completion in background
	go func() {
		wg.Wait()
		close(jobs)
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
	case <-done:
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

// ScanRemote scans a remote target via the Agent API
// It strictly requires DEST_HOST to be set and the receiver to be reachable via HTTP.
func (s *Scanner) ScanRemote(uri string) (*Manifest, error) {
	uriHost, remotePath := ParseRemoteDestination(uri)

	destHost := uriHost
	if destHost == "" {
		destHost = os.Getenv("DEST_HOST")
	}

	if destHost == "" {
		return nil, fmt.Errorf("remote scan failed: could not determine destination host from URI %q or DEST_HOST environment variable", uri)
	}

	if remotePath == "" {
		// If ParseRemoteDestination couldn't find a path, it might be just a module name without path
		// or an invalid format. Let's try to extract at least something.
		if strings.Contains(uri, "::") {
			parts := strings.SplitN(uri, "::", 2)
			if len(parts) > 1 {
				remotePath = parts[1]
			}
		} else if strings.HasPrefix(uri, "rsync://") {
			pathPart := strings.TrimPrefix(uri, "rsync://")
			idx := strings.Index(pathPart, "/")
			if idx != -1 {
				remotePath = pathPart[idx+1:]
			}
		}
	}

	apiURL := fmt.Sprintf("http://%s:8080/api/manifest?path=%s", destHost, url.QueryEscape(remotePath))

	log.Printf("[Scanner] Requesting remote manifest from API: %s", apiURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to contact receiver API at %s: %w", destHost, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[Scanner] Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("receiver API returned status %s", resp.Status)
	}

	manifest := &Manifest{}
	if err := json.NewDecoder(resp.Body).Decode(manifest); err != nil {
		log.Printf("[Scanner] Failed to decode manifest from %s: %v", apiURL, err)
		return nil, fmt.Errorf("failed to decode manifest JSON: %w", err)
	}

	log.Printf("[Scanner] Successfully received %d items from %s", len(manifest.Files)+len(manifest.Dirs), apiURL)
	return manifest, nil
}
