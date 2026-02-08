package sync

import (
	"fmt"
	"io"
	"log"
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
	// Construct rsync command
	// rsync -av --partial <src> <dst>
	args := []string{"-av", "--partial"}
	if t.opts.BandwidthLimit > 0 {
		// Convert bytes/s to Kbit/s roughly or use specialized logic.
		// Rsync --bwlimit is in KBytes per second
		kbps := t.opts.BandwidthLimit / 1024
		if kbps > 0 {
			args = append(args, fmt.Sprintf("--bwlimit=%d", kbps))
		}
	}
	args = append(args, src, dst)

	cmd := exec.Command("rsync", args...)
	cmd.Env = os.Environ()
	if pass := os.Getenv("RSYNC_PASSWORD"); pass != "" {
		cmd.Env = append(cmd.Env, "RSYNC_PASSWORD="+pass)
	}

	// Capture output for logging? Or just run it.
	// Ideally we parse progress, but rsync progress parsing is complex.
	// For now, let's just run it.
	// Maybe we can assume success = 100% progress for the file.

	if err := cmd.Run(); err != nil {
		if t.opts.OnComplete != nil {
			t.opts.OnComplete(filepath.Base(src), 0, err)
		}
		return fmt.Errorf("rsync command failed: %w", err)
	}

	// On success
	fi, _ := os.Stat(src)
	if t.opts.OnComplete != nil {
		t.opts.OnComplete(filepath.Base(src), fi.Size(), nil)
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
		// Deletion via rsync requires sending a delete list or using a specialized command?
		// Actually, rsync delete is usually part of sync.
		// But here we are deleting specific files.
		// Rsync doesn't have a direct "delete single file" command without syncing a directory.
		// We might need to handle this differently or skip for now.
		// If we sync an empty source to it... dangerous.
		// For now, let's log warning and return nil to avoid crashing,
		// but acknowledging we can't delete remote files easily without SSH.
		log.Printf("[Transferer] Warning: Deleting remote files is not fully supported via rsync:// yet: %s", path)
		return nil
	}
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
func (t *Transferer) DeleteDir(path string) error {
	if strings.Contains(path, "::") || strings.HasPrefix(path, "rsync://") {
		log.Printf("[Transferer] Warning: Deleting remote dirs is not fully supported via rsync:// yet: %s", path)
		return nil
	}
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
func (t *Transferer) RenameFile(oldPath, newPath string) error {
	dstDir := filepath.Dir(newPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}
func (t *Transferer) SetBandwidthLimit(limit int64) { t.opts.BandwidthLimit = limit }
