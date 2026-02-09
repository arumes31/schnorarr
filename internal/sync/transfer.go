package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"schnorarr/internal/sync/pool"
)

const (
	// ParallelThreshold is the file size (100MB) above which we use multi-streaming
	ParallelThreshold = 100 * 1024 * 1024
	// DefaultNumStreams is the number of parallel streams for large files
	DefaultNumStreams = 4
	// ChunkSize is the read/write buffer size
	ChunkSize = 128 * 1024 // 128KB
)

// TransferOptions configures file transfer behavior
type TransferOptions struct {
	// BandwidthLimit in bytes per second (0 = unlimited)
	BandwidthLimit int64
	// OnProgress callback for transfer progress updates
	OnProgress func(path string, bytesTransferred, totalBytes int64)
	// OnComplete callback when transfer completes
	OnComplete func(path string, size int64, err error)
	// CheckPaused returns true if the transfer should be interrupted
	CheckPaused func() bool
}

// Transferer handles file transfer operations
type Transferer struct {
	opts TransferOptions
}

// NewTransferer creates a new file transferer
func NewTransferer(opts TransferOptions) *Transferer {
	return &Transferer{opts: opts}
}

// CopyFile copies a file from src to dst with bandwidth limiting and progress reporting
func (t *Transferer) CopyFile(src, dst string) error {
	if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
		return fmt.Errorf("transfer interrupted by pause")
	}
	pool.Acquire()
	defer pool.Release()

	log.Printf("[Transferer] Copying %s -> %s", src, dst)

	// Check for remote destination
	if strings.Contains(dst, "::") || strings.HasPrefix(dst, "rsync://") {
		return t.copyRemote(src, dst)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			log.Printf("[Transferer] Error closing source file: %v", err)
		}
	}()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	totalSize := srcInfo.Size()
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpDst := dst + ".tmp"

	// We only support parallel transfers for new files > threshold
	// Resumption currently falls back to sequential for simplicity
	useParallel := totalSize > ParallelThreshold && t.opts.BandwidthLimit == 0

	var bytesTransferred int64
	var copyErr error

	// Retry logic
	maxRetries := 3
	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			sleep := time.Duration(1<<uint(i)) * time.Second
			log.Printf("[Transferer] Retry %d/%d for %s...", i, maxRetries, src)
			time.Sleep(sleep)

			// Reset for retry
			if _, err := srcFile.Seek(0, io.SeekStart); err != nil {
				copyErr = fmt.Errorf("failed to seek to start: %w", err)
				break
			}
			bytesTransferred = 0
		}

		dstFile, err := os.Create(tmpDst)
		if err != nil {
			copyErr = err
			continue
		}

		if useParallel {
			bytesTransferred, copyErr = t.copyParallel(filepath.Base(src), srcFile, dstFile, totalSize)
		} else {
			// Sequential copy (used for small files or bandwidth limited transfers)
			if t.opts.BandwidthLimit > 0 {
				bytesTransferred, copyErr = t.copyWithBandwidthLimit(filepath.Base(src), srcFile, dstFile, totalSize, 0)
			} else {
				bytesTransferred, copyErr = t.copyWithProgress(filepath.Base(src), srcFile, dstFile, totalSize, 0)
			}
		}

		if err := dstFile.Sync(); err != nil {
			log.Printf("[Transferer] Warning: failed to sync destination file: %v", err)
		}
		if err := dstFile.Close(); err != nil {
			log.Printf("[Transferer] Error closing destination file: %v", err)
		}

		if copyErr == nil {
			break
		}

		if copyErr.Error() == "transfer interrupted by pause" {
			break
		}
		log.Printf("[Transferer] Attempt %d failed: %v", i+1, copyErr)
	}

	if copyErr != nil {
		if t.opts.OnComplete != nil {
			t.opts.OnComplete(filepath.Base(src), bytesTransferred, copyErr)
		}
		_ = os.Remove(tmpDst) // Cleanup temp file
		return copyErr
	}

	if err := os.Chtimes(tmpDst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		log.Printf("[Transferer] Warning: failed to set file times: %v", err)
	}
	if err := os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	log.Printf("[Transferer] Successfully copied %s (%d bytes)", src, bytesTransferred)
	if t.opts.OnComplete != nil {
		t.opts.OnComplete(filepath.Base(src), bytesTransferred, nil)
	}
	return nil
}

// copyRemote uses the rsync command to transfer a file to a remote destination
func (t *Transferer) copyRemote(src, dst string) error {
	if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
		return fmt.Errorf("transfer interrupted by pause")
	}
	// Root paths in Docker/Linux should already use forward slashes.
	// But ensure we don't have backslashes from legacy Windows-style configs.
	src = filepath.ToSlash(src)

	// Normalize remote destination path
	if strings.Contains(dst, "::") || strings.HasPrefix(dst, "rsync://") {
		// Force forward slashes in the path part of the URI
		if strings.Contains(dst, "::") {
			parts := strings.SplitN(dst, "::", 2)
			dst = parts[0] + "::" + strings.ReplaceAll(parts[1], "\\", "/")
		} else {
			parts := strings.SplitN(dst, "rsync://", 2)
			dst = "rsync://" + strings.ReplaceAll(parts[1], "\\", "/")
		}
	}

	// Get file size for progress tracking
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	totalSize := fi.Size()

	// Construct args:
	// -a: archive mode
	// --inplace: update destination files in-place
	// --append-verify: resume interrupted transfers and verify checksums
	// --protect-args: handles spaces and special chars in paths correctly with daemon protocol
	// --mkpath: create missing parent directories on destination (rsync 3.2.3+)
	args := []string{"-a", "--inplace", "--append-verify", "--protect-args", "--mkpath"}

	if t.opts.BandwidthLimit > 0 {
		kbps := t.opts.BandwidthLimit / 1024
		if kbps > 0 {
			args = append(args, fmt.Sprintf("--bwlimit=%d", kbps))
		}
	}
	args = append(args, src, dst)

	// Parse destination to get host and remote path for size monitoring
	destHost, remotePath := ParseRemoteDestination(dst)
	log.Printf("[Transferer] DEBUG: Parsed destination - host: %q, path: %q", destHost, remotePath)

	maxRetries := 3
	stuckThreshold := 60 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[Transferer] Retry %d/%d for %s (previous attempt stuck or failed)...", attempt, maxRetries, src)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		log.Printf("[Transferer] Executing rsync: %s", strings.Join(args, " "))
		cmd := exec.Command("rsync", args...)
		cmd.Env = os.Environ()
		if pass := os.Getenv("RSYNC_PASSWORD"); pass != "" {
			cmd.Env = append(cmd.Env, "RSYNC_PASSWORD="+pass)
		}

		// Start rsync in background
		if err := cmd.Start(); err != nil {
			if attempt == maxRetries {
				return fmt.Errorf("failed to start rsync: %w", err)
			}
			continue
		}

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		ticker := time.NewTicker(1 * time.Second)
		lastReportedSize := int64(0)
		lastProgressTime := time.Now()
		isStuck := false

		for !isStuck {
			select {
			case err := <-done:
				ticker.Stop()
				// Rsync completed
				if err != nil {
					log.Printf("[Transferer] rsync failed for %s: %v", src, err)
					if attempt == maxRetries {
						if t.opts.OnComplete != nil {
							t.opts.OnComplete(filepath.Base(src), 0, fmt.Errorf("rsync error: %w", err))
						}
						return fmt.Errorf("rsync command failed: %w", err)
					}
					// Exit the inner loop to retry
					isStuck = true
					break
				}

				// Final progress update with total size
				if t.opts.OnProgress != nil && totalSize > lastReportedSize {
					log.Printf("[Transferer] DEBUG: Final progress update - %d/%d bytes", totalSize, totalSize)
					t.opts.OnProgress(src, totalSize, totalSize)
				}

				log.Printf("[Transferer] Successfully transferred %s (%d bytes)", src, totalSize)
				if t.opts.OnComplete != nil {
					t.opts.OnComplete(filepath.Base(src), totalSize, nil)
				}
				return nil

			case <-ticker.C:
				// Check if transfer should be paused
				if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
					ticker.Stop()
					log.Printf("[Transferer] Transfer paused for %s, killing rsync...", src)
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					return fmt.Errorf("transfer interrupted by pause")
				}

				// Poll destination file size (only if not already at 100%)
				if destHost != "" && remotePath != "" && lastReportedSize < totalSize {
					currentSize := getRemoteFileSize(destHost, remotePath)
					if currentSize > 0 && currentSize != lastReportedSize {
						if t.opts.OnProgress != nil {
							t.opts.OnProgress(src, currentSize, totalSize)
						}
						lastReportedSize = currentSize
						lastProgressTime = time.Now()
					}
				}

				// If we are at 100%, keep updating the progress time to prevent "stuck" timeout
				// while rsync is doing its final verification/cleanup.
				if lastReportedSize >= totalSize {
					lastProgressTime = time.Now()
					log.Printf("[Transferer] DEBUG: Already at 100%% (%d/%d), updating lastProgressTime for %s", lastReportedSize, totalSize, src)
				}

				// Check if stuck
				if time.Since(lastProgressTime) > stuckThreshold {
					log.Printf("[Transferer] WARNING: rsync seems stuck for %s (no progress for %v). Killing process...", src, stuckThreshold)
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					isStuck = true
					ticker.Stop()
				}
			}
		}
	}

	return fmt.Errorf("rsync failed after %d retries", maxRetries)
}

// ParseRemoteDestination extracts host and path from rsync destination
func ParseRemoteDestination(dst string) (host, remotePath string) {
	// Handle rsync://host/module/path format
	if strings.HasPrefix(dst, "rsync://") {
		pathPart := strings.TrimPrefix(dst, "rsync://")
		// Remove user@ if present
		if idx := strings.Index(pathPart, "@"); idx != -1 {
			pathPart = pathPart[idx+1:]
		}
		// Find first slash which separates host from module/path
		idx := strings.Index(pathPart, "/")
		if idx == -1 {
			return pathPart, ""
		}
		host = pathPart[:idx]
		// Remove user@ if present in the host part (extra safety)
		if uIdx := strings.Index(host, "@"); uIdx != -1 {
			host = host[uIdx+1:]
		}
		// Remove port if present
		if portIdx := strings.Index(host, ":"); portIdx != -1 {
			host = host[:portIdx]
		}

		modulePath := pathPart[idx+1:]
		// Split module and path
		pathParts := strings.SplitN(modulePath, "/", 2)
		if len(pathParts) > 1 {
			remotePath = pathParts[1]
		}
		return host, remotePath
	}

	// Handle user@host::module/path format
	if strings.Contains(dst, "::") {
		parts := strings.SplitN(dst, "::", 2)
		hostPart := parts[0]
		// Extract just the hostname (remove user@ if present)
		if strings.Contains(hostPart, "@") {
			hostPart = strings.SplitN(hostPart, "@", 2)[1]
		}

		// Extract path from module/path
		if len(parts) > 1 {
			modulePath := parts[1]
			// Split module and path
			pathParts := strings.SplitN(modulePath, "/", 2)
			if len(pathParts) > 1 {
				remotePath = pathParts[1]
			}
		}
		return hostPart, remotePath
	}
	return "", ""
}

// getRemoteFileSize queries the receiver's /api/stat endpoint for file size
func getRemoteFileSize(host, path string) int64 {
	apiURL := fmt.Sprintf("http://%s:8080/api/stat?path=%s", host, url.QueryEscape(path))
	log.Printf("[Transferer] DEBUG: Querying stat API: %s", apiURL)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(apiURL)
	if err != nil {
		log.Printf("[Transferer] DEBUG: API request failed: %v", err)
		return 0
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[Transferer] Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Transferer] DEBUG: API returned status %d", resp.StatusCode)
		return 0
	}

	var statResp struct {
		Size   int64 `json:"size"`
		Exists bool  `json:"exists"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statResp); err != nil {
		log.Printf("[Transferer] DEBUG: Failed to decode response: %v", err)
		return 0
	}

	log.Printf("[Transferer] DEBUG: API response - exists: %v, size: %d", statResp.Exists, statResp.Size)
	return statResp.Size
}

func (t *Transferer) copyParallel(filename string, srcFile, dstFile *os.File, totalSize int64) (int64, error) {
	numStreams := DefaultNumStreams
	chunkSize := (totalSize + int64(numStreams) - 1) / int64(numStreams)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	var totalWritten int64
	var mu sync.Mutex

	log.Printf("[Transferer] Starting parallel transfer with %d streams for %s", numStreams, filename)

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(streamID int) {
			defer wg.Done()

			start := int64(streamID) * chunkSize
			end := start + chunkSize
			if end > totalSize {
				end = totalSize
			}

			if start >= totalSize {
				return
			}

			buf := make([]byte, ChunkSize)
			offset := start

			for offset < end {
				if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
					errOnce.Do(func() { firstErr = fmt.Errorf("transfer interrupted by pause") })
					return
				}

				toRead := int64(len(buf))
				if offset+toRead > end {
					toRead = end - offset
				}

				nr, err := srcFile.ReadAt(buf[:toRead], offset)
				if nr > 0 {
					nw, ew := dstFile.WriteAt(buf[:nr], offset)
					if ew != nil {
						errOnce.Do(func() { firstErr = ew })
						return
					}

					mu.Lock()
					totalWritten += int64(nw)
					currentTotal := totalWritten
					mu.Unlock()

					if t.opts.OnProgress != nil {
						t.opts.OnProgress(filename, currentTotal, totalSize)
					}
					offset += int64(nw)
				}
				if err != nil && err != io.EOF && offset < end {
					errOnce.Do(func() { firstErr = err })
					return
				}
				if nr == 0 {
					break
				}
			}
		}(i)
	}

	wg.Wait()
	return totalWritten, firstErr
}

func (t *Transferer) copyWithProgress(filename string, src io.Reader, dst io.Writer, totalSize, offset int64) (int64, error) {
	buf := make([]byte, ChunkSize)
	var written int64
	for {
		if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
			return written, fmt.Errorf("transfer interrupted by pause")
		}
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				if t.opts.OnProgress != nil {
					t.opts.OnProgress(filename, offset+written, totalSize)
				}
			}
			if ew != nil {
				return written, ew
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (t *Transferer) copyWithBandwidthLimit(filename string, src io.Reader, dst io.Writer, totalSize, offset int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	sleepDuration := time.Duration(float64(len(buf)) / float64(t.opts.BandwidthLimit) * float64(time.Second))
	lastTime := time.Now()

	for {
		if t.opts.CheckPaused != nil && t.opts.CheckPaused() {
			return written, fmt.Errorf("transfer interrupted by pause")
		}
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				if t.opts.OnProgress != nil {
					t.opts.OnProgress(filename, offset+written, totalSize)
				}
				elapsed := time.Since(lastTime)
				if elapsed < sleepDuration {
					time.Sleep(sleepDuration - elapsed)
				}
				lastTime = time.Now()
			}
			if ew != nil {
				return written, ew
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (t *Transferer) CreateDir(path string) error {
	if strings.Contains(path, "::") || strings.HasPrefix(path, "rsync://") {
		// Rsync creates dirs implicitly during transfer, or we can assume it exists?
		// Explicit mkdir is hard without ssh.
		// Usually we can skip mkdir for rsync targets as rsync -r handles it.
		return nil
	}
	return os.MkdirAll(path, 0755)
}
func (t *Transferer) DeleteFile(path string) error {
	if strings.Contains(path, "::") || strings.HasPrefix(path, "rsync://") {
		return t.deleteRemote(path, false)
	}
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

func (t *Transferer) DeleteDir(path string) error {
	if strings.Contains(path, "::") || strings.HasPrefix(path, "rsync://") {
		return t.deleteRemote(path, true)
	}
	err := os.RemoveAll(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

func (t *Transferer) deleteRemote(uri string, isDir bool) error {
	destHost := os.Getenv("DEST_HOST")
	if destHost == "" {
		return fmt.Errorf("remote delete failed: DEST_HOST not set")
	}

	parts := strings.Split(uri, "::")
	if len(parts) < 2 {
		return fmt.Errorf("invalid rsync URI format: %s", uri)
	}
	remotePath := parts[1]

	apiURL := fmt.Sprintf("http://%s:8080/api/delete?path=%s&dir=%v",
		destHost, url.QueryEscape(remotePath), isDir)

	log.Printf("[Transferer] Requesting remote delete: %s", apiURL)

	resp, err := http.Post(apiURL, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to contact receiver API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("receiver API returned status %s", resp.Status)
	}

	log.Printf("[Transferer] Remote delete successful: %s", remotePath)
	return nil
}

func (t *Transferer) RenameFile(oldPath, newPath string) error {
	if strings.Contains(oldPath, "::") || strings.HasPrefix(oldPath, "rsync://") ||
		strings.Contains(newPath, "::") || strings.HasPrefix(newPath, "rsync://") {
		return fmt.Errorf("rename not supported for remote targets")
	}

	dstDir := filepath.Dir(newPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	err := os.Rename(oldPath, newPath)
	if err == nil {
		return nil
	}

	// Fallback for cross-device rename: Copy then Delete
	log.Printf("[Transferer] Rename failed (%v), falling back to copy+delete for %s -> %s", err, oldPath, newPath)
	if err := t.CopyFile(oldPath, newPath); err != nil {
		return fmt.Errorf("fallback copy failed: %w", err)
	}

	return os.Remove(oldPath)
}
func (t *Transferer) SetBandwidthLimit(limit int64) { t.opts.BandwidthLimit = limit }
