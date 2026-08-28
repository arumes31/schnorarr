package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening scan root: %w", err)
	}
	defer func() { _ = rootFS.Close() }()

	manifest := NewManifest(root)
	log.Printf("[Scanner] Starting parallel scan of %s", root)
	var mu sync.Mutex
	const numWorkers = 8
	jobs := make(chan string, 10000)
	var jobsWG sync.WaitGroup
	var workersWG sync.WaitGroup
	errCh := make(chan error, 1)
	done := make(chan struct{})
	var errOnce sync.Once

	worker := func() {
		defer workersWG.Done()
		for dir := range jobs {
			func() {
				defer jobsWG.Done()
				select {
				case <-done:
					return
				default:
				}

				directory, err := rootFS.Open(dir)
				if err != nil {
					errOnce.Do(func() {
						errCh <- fmt.Errorf("opening directory %q: %w", dir, err)
						close(done)
					})
					return
				}
				entries, err := directory.ReadDir(-1)
				closeErr := directory.Close()
				if err != nil {
					errOnce.Do(func() {
						errCh <- fmt.Errorf("reading directory %q: %w", dir, err)
						close(done)
					})
					return
				}
				if closeErr != nil {
					log.Printf("[Scanner] Directory close error: %v", closeErr)
				}

				for _, d := range entries {
					select {
					case <-done:
						return
					default:
					}
					relPath := filepath.Join(dir, d.Name())
					info, err := rootFS.Lstat(relPath)
					if err != nil || info.Mode()&os.ModeSymlink != 0 {
						continue
					}
					if s.shouldExclude(relPath) {
						continue
					}
					if !info.IsDir() && !s.shouldInclude(relPath) {
						continue
					}
					fileInfo := &FileInfo{
						Path:    filepath.ToSlash(relPath),
						Size:    info.Size(),
						ModTime: info.ModTime(),
						IsDir:   info.IsDir(),
					}

					if s.ComputeHashes && !info.IsDir() {
						file, openErr := rootFS.Open(relPath)
						if openErr != nil {
							log.Printf("[Scanner] Hash open error for %q: %v", relPath, openErr)
						} else {
							hashErr := fileInfo.ComputeHash(file)
							closeErr := file.Close()
							if hashErr != nil {
								log.Printf("[Scanner] Hash error for %q: %v", relPath, hashErr)
							}
							if closeErr != nil {
								log.Printf("[Scanner] Hash file close error: %v", closeErr)
							}
						}
					}

					mu.Lock()
					manifest.Add(fileInfo)
					mu.Unlock()

					if info.IsDir() {
						jobsWG.Add(1)
						select {
						case jobs <- relPath:
						case <-done:
							jobsWG.Done()
						}
					}
				}
			}()
		}
	}

	for i := 0; i < numWorkers; i++ {
		workersWG.Add(1)
		go worker()
	}

	jobsWG.Add(1)
	jobs <- "."
	scanDone := make(chan struct{})
	go func() {
		jobsWG.Wait()
		close(jobs)
		workersWG.Wait()
		close(scanDone)
	}()

	select {
	case err := <-errCh:
		<-scanDone
		return nil, err
	case <-scanDone:
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

// ScanRemote scans a remote target through the authenticated HTTPS receiver API.
func (s *Scanner) ScanRemote(uri string) (*Manifest, error) {
	_, remotePath := ParseRemoteDestination(uri)

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
	if remotePath == "" {
		remotePath = "."
	}
	log.Printf("[Scanner] Requesting remote manifest")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := receiverAPIRequest(ctx, http.MethodGet, "/api/manifest", url.Values{"path": {remotePath}}, 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to contact receiver API: %w", err)
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(manifest); err != nil {
		log.Printf("[Scanner] Failed to decode manifest: %v", err)
		return nil, fmt.Errorf("failed to decode manifest JSON: %w", err)
	}

	log.Printf("[Scanner] Successfully received %d items", len(manifest.Files)+len(manifest.Dirs))
	return manifest, nil
}
