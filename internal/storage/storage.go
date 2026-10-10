// Package storage checks share identity and access before sync work starts.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MarkerName = ".schnorarr-shared-token"
const ProbePrefix = ".schnorarr-probe-"
const Timeout = 5 * time.Second

var ErrUnavailable = errors.New("storage unavailable")

type Share struct {
	Path string
	ID   string
}

// Checker allows only one probe at a time. A stuck SMB syscall can outlive a
// deadline; subsequent callers wait instead of spawning more probe goroutines.
// Successful checks are never cached.
type Checker struct {
	shares   []Share
	writable bool
	slot     chan struct{}
	boundary string
}

func New(shares []Share, writable bool) (*Checker, error) {
	if len(shares) == 0 {
		return nil, fmt.Errorf("a folder and shared token are required")
	}
	for _, share := range shares {
		if !filepath.IsAbs(share.Path) || strings.TrimSpace(share.ID) == "" {
			return nil, fmt.Errorf("each storage check requires an absolute path and a nonempty share ID")
		}
	}
	return &Checker{shares: append([]Share(nil), shares...), writable: writable, slot: make(chan struct{}, 1)}, nil
}

func (c *Checker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	select {
	case c.slot <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
	done := make(chan error, 1)
	go func() {
		defer func() { <-c.slot }()
		for _, share := range c.shares {
			if c.boundary != "" {
				root, rootErr := filepath.EvalSymlinks(c.boundary)
				folder, folderErr := filepath.EvalSymlinks(share.Path)
				if err := errors.Join(rootErr, folderErr); err != nil {
					done <- fmt.Errorf("%w: resolve receiver folder: %w", ErrUnavailable, err)
					return
				}
				if !within(root, folder) {
					done <- fmt.Errorf("%w: folder is outside receiver storage", ErrUnavailable)
					return
				}
			}
			if err := checkShare(ctx, share, c.writable); err != nil {
				done <- fmt.Errorf("%w: %s: %w", ErrUnavailable, share.Path, err)
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
}

func checkShare(ctx context.Context, share Share, writable bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	marker, err := os.Open(filepath.Join(share.Path, MarkerName))
	if err != nil {
		return fmt.Errorf("read share marker: %w", err)
	}
	content, readErr := io.ReadAll(io.LimitReader(marker, 4097))
	closeErr := marker.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("read share marker: %w", err)
	}
	if len(content) > 4096 || strings.TrimSpace(string(content)) != share.ID {
		return fmt.Errorf("shared token does not match this engine")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.Open(share.Path)
	if err != nil {
		return err
	}
	_, readErr = dir.ReadDir(1)
	closeErr = dir.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("read share directory: %w", err)
	}
	if !writable {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	probe, err := os.CreateTemp(share.Path, ProbePrefix)
	if err != nil {
		return fmt.Errorf("create storage probe: %w", err)
	}
	_, writeErr := probe.WriteString("schnorarr storage readiness\n")
	syncErr := probe.Sync()
	closeErr = probe.Close()
	removeErr := os.Remove(probe.Name())
	if err := errors.Join(writeErr, syncErr, closeErr, removeErr, ctx.Err()); err != nil {
		return fmt.Errorf("write storage probe: %w", err)
	}
	return nil
}

// Reserved protects markers and probes even with user-supplied include rules.
func Reserved(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == MarkerName || part == ".schnorarr-share-id" || strings.HasPrefix(part, ProbePrefix) {
			return true
		}
	}
	return false
}

// Protects rejects deleting a share root or an ancestor containing its marker.
func (c *Checker) Protects(path string) bool {
	if Reserved(path) {
		return true
	}
	for _, share := range c.shares {
		rel, err := filepath.Rel(path, share.Path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
