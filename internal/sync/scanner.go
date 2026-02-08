package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// ScanRemote scans a remote rsync target first by trying the Agent API, then falling back to rsync
func (s *Scanner) ScanRemote(uri string) (*Manifest, error) {
	// 1. Try API if DEST_HOST is available
	destHost := os.Getenv("DEST_HOST")
	if destHost != "" && !strings.HasPrefix(uri, "http") {
		// Construct API URL
		// URI is like user@host::module/path
		// We extract the path part: module/path
		// Regex or string splitting

		// Expected URI: user@host::module/path
		parts := strings.Split(uri, "::")
		if len(parts) > 1 {
			remotePath := parts[1] // module/path
			apiURL := fmt.Sprintf("http://%s:8080/api/manifest?path=%s", destHost, remotePath)

			log.Printf("[Scanner] Requesting remote manifest from API: %s", apiURL)

			client := &http.Client{Timeout: 2 * time.Minute}
			resp, err := client.Get(apiURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				manifest := &Manifest{}
				if err := json.NewDecoder(resp.Body).Decode(manifest); err == nil {
					log.Printf("[Scanner] Received valid manifest from API: %d files", len(manifest.Files))
					return manifest, nil
				} else {
					log.Printf("[Scanner] Failed to decode API manifest: %v", err)
				}
			} else {
				log.Printf("[Scanner] API request failed (trying rsync fallback): %v", err)
			}
		}
	}

	// 2. Fallback to Rsync CLI (Legacy/Non-Agent)
	manifest := NewManifest(uri)
	log.Printf("[Scanner] Scanning remote target via rsync-cli: %s", uri)

	// Set timeout for the scan
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rsync", "--list-only", "-r", uri)

	// Pass RSYNC_PASSWORD if set
	cmd.Env = os.Environ()
	if pass := os.Getenv("RSYNC_PASSWORD"); pass != "" {
		cmd.Env = append(cmd.Env, "RSYNC_PASSWORD="+pass)
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("rsync scan failed: %v, stderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("rsync scan failed: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// Mode is roughly fields[0]
		mode := fields[0]
		isDir := strings.HasPrefix(mode, "d")

		// Size is fields[1], remove commas
		sizeStr := strings.ReplaceAll(fields[1], ",", "")
		size, _ := strconv.ParseInt(sizeStr, 10, 64)

		// Simple heuristics: find the date/time combo and take everything after
		// Looking for YYYY/MM/DD
		var path string
		for i, f := range fields {
			if strings.Contains(f, "/") && strings.Count(f, "/") == 2 && len(f) == 10 {
				// Found date at index i
				// Time should be at i+1
				if i+2 < len(fields) {
					pathParts := fields[i+2:]
					path = strings.Join(pathParts, " ")
				}
				break
			}
		}

		if path == "" || path == "." || path == "./" {
			continue
		}

		// Clean path
		path = strings.TrimPrefix(path, "./")

		// Apply Filters
		if s.shouldExclude(path) {
			continue
		}
		if !isDir && !s.shouldInclude(path) {
			continue
		}

		fileInfo := &FileInfo{
			Path:  filepath.ToSlash(path),
			Size:  size,
			IsDir: isDir,
		}

		manifest.Add(fileInfo)
	}

	log.Printf("[Scanner] Finished remote scan of %s: found %d items", uri, len(manifest.Files)+len(manifest.Dirs))
	return manifest, nil
}
