package sync

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// --partial: keep partially transferred files
	// --protect-args: handles spaces and special chars in paths correctly with daemon protocol
	// --mkpath: create missing parent directories on destination (rsync 3.2.3+)
	// --progress: show progress during transfer (daemon-compatible)
	args := []string{"-a", "--partial", "--protect-args", "--mkpath", "--progress"}

	if t.opts.BandwidthLimit > 0 {
		kbps := t.opts.BandwidthLimit / 1024
		if kbps > 0 {
			args = append(args, fmt.Sprintf("--bwlimit=%d", kbps))
		}
	}
	args = append(args, src, dst)

	log.Printf("[Transferer] Executing rsync: %s", strings.Join(args, " "))

	cmd := exec.Command("rsync", args...)
	cmd.Env = os.Environ()
	if pass := os.Getenv("RSYNC_PASSWORD"); pass != "" {
		cmd.Env = append(cmd.Env, "RSYNC_PASSWORD="+pass)
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rsync: %w", err)
	}

	// Combine stdout and stderr for progress parsing
	// rsync sends progress to stderr by default
	combinedOutput := io.MultiReader(stdout, stderr)

	// Read output byte-by-byte to handle \r (carriage return) from --progress
	var currentLine strings.Builder
	var lastProgress int64
	buf := make([]byte, 1)
	for {
		n, err := combinedOutput.Read(buf)
		if n > 0 {
			ch := buf[0]
			if ch == '\r' || ch == '\n' {
				// End of a progress line, parse it
				line := strings.TrimSpace(currentLine.String())
				if line != "" {
					log.Printf("[Transferer] DEBUG: rsync output line: %q", line)
					// Parse progress format: "    1,234,567  12%  123.45kB/s    0:01:23"
					// or progress2 format: "     123,456,789  45%  123.45MB/s    0:00:12"
					// Both formats have bytes as the first field
					// Only parse lines that start with numbers (skip headers like "sending incremental file list")
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						// First field is bytes transferred (with commas)
						bytesStr := strings.ReplaceAll(fields[0], ",", "")
						// Check if first field is actually a number
						if bytes, parseErr := strconv.ParseInt(bytesStr, 10, 64); parseErr == nil && bytes > 0 {
							log.Printf("[Transferer] DEBUG: Parsed %d bytes transferred out of %d total", bytes, totalSize)
							if t.opts.OnProgress != nil && bytes != lastProgress {
								log.Printf("[Transferer] DEBUG: Calling OnProgress(%s, %d, %d)", src, bytes, totalSize)
								t.opts.OnProgress(src, bytes, totalSize)
								lastProgress = bytes
							}
						} else {
							log.Printf("[Transferer] DEBUG: Skipping non-progress line: %q", line)
						}
					}
				}
				currentLine.Reset()
			} else {
				currentLine.WriteByte(ch)
			}
		}
		if err != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("[Transferer] rsync failed for %s: %v", src, err)
		if t.opts.OnComplete != nil {
			t.opts.OnComplete(filepath.Base(src), 0, fmt.Errorf("rsync error: %v", err))
		}
		return fmt.Errorf("rsync command failed: %w", err)
	}

	log.Printf("[Transferer] Successfully transferred %s (%d bytes)", src, totalSize)
	if t.opts.OnComplete != nil {
		t.opts.OnComplete(filepath.Base(src), totalSize, nil)
	}
	return nil
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
