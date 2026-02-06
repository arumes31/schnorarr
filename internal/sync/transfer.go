package sync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TransferOptions configures file transfer behavior
type TransferOptions struct {
	// BandwidthLimit in bytes per second (0 = unlimited)
	BandwidthLimit int64
	// OnProgress callback for transfer progress updates
	OnProgress func(path string, bytesTransferred, totalBytes int64)
	// OnComplete callback when transfer completes
	OnComplete func(path string, size int64, err error)
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
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Get source file info
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create destination directory if needed
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create temporary file for atomic write
	tmpDst := dst + ".tmp"
	dstFile, err := os.Create(tmpDst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}

	// Copy with bandwidth limiting and progress reporting
	var bytesTransferred int64
	var copyErr error

	if t.opts.BandwidthLimit > 0 {
		bytesTransferred, copyErr = t.copyWithBandwidthLimit(srcFile, dstFile, srcInfo.Size())
	} else {
		bytesTransferred, copyErr = t.copyWithProgress(srcFile, dstFile, srcInfo.Size())
	}

	dstFile.Close()

	if copyErr != nil {
		os.Remove(tmpDst)
		if t.opts.OnComplete != nil {
			t.opts.OnComplete(dst, 0, copyErr)
		}
		return copyErr
	}

	// Preserve modification time
	if err := os.Chtimes(tmpDst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("failed to set file times: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpDst, dst); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	if t.opts.OnComplete != nil {
		t.opts.OnComplete(dst, bytesTransferred, nil)
	}

	return nil
}

// copyWithProgress copies data with progress reporting
func (t *Transferer) copyWithProgress(src io.Reader, dst io.Writer, totalSize int64) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64

	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				if t.opts.OnProgress != nil {
					t.opts.OnProgress("", written, totalSize)
				}
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
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

// copyWithBandwidthLimit copies data with bandwidth limiting and progress reporting
func (t *Transferer) copyWithBandwidthLimit(src io.Reader, dst io.Writer, totalSize int64) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64

	// Calculate sleep duration based on bandwidth limit
	// Target: BandwidthLimit bytes per second
	bytesPerChunk := int64(len(buf))
	sleepDuration := time.Duration(float64(bytesPerChunk) / float64(t.opts.BandwidthLimit) * float64(time.Second))

	lastTime := time.Now()

	for {
		nr, err := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				if t.opts.OnProgress != nil {
					t.opts.OnProgress("", written, totalSize)
				}

				// Bandwidth throttling
				elapsed := time.Since(lastTime)
				if elapsed < sleepDuration {
					time.Sleep(sleepDuration - elapsed)
				}
				lastTime = time.Now()
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
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

// CreateDir creates a directory
func (t *Transferer) CreateDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return nil
}

// DeleteFile deletes a file
func (t *Transferer) DeleteFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

// DeleteDir deletes a directory (must be empty)
func (t *Transferer) DeleteDir(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete directory %s: %w", path, err)
	}
	return nil
}

// SetBandwidthLimit updates the bandwidth limit
func (t *Transferer) SetBandwidthLimit(limit int64) {
	t.opts.BandwidthLimit = limit
}
