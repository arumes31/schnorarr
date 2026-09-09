package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrProtected = errors.New("shared token folders are protected")

// Receiver remembers successfully verified folder/token pairs outside the
// share so the rsync hook can independently verify the expected token.
type Receiver struct {
	Root     string
	Registry string
	mu       sync.Mutex
	slot     chan struct{}
}

func ReceiverFromEnv() *Receiver {
	root := os.Getenv("SOURCE_DIR")
	if root == "" {
		root = "/data"
	}
	config := "/config"
	if path := os.Getenv("DB_PATH"); path != "" {
		config = filepath.Dir(path)
	}
	return NewReceiver(root, filepath.Join(config, "storage-tokens.json"))
}

func NewReceiver(root, registry string) *Receiver {
	return &Receiver{Root: filepath.Clean(root), Registry: registry, slot: make(chan struct{}, 1)}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (r *Receiver) Resolve(path string) (string, error) {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return "", fmt.Errorf("invalid receiver folder")
	}
	full := filepath.Join(r.Root, filepath.FromSlash(path))
	if !within(r.Root, full) || Reserved(full) {
		return "", fmt.Errorf("invalid receiver folder")
	}
	return full, nil
}

func (r *Receiver) bindings() (map[string]string, error) {
	data, err := os.ReadFile(r.Registry)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var bindings map[string]string
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, err
	}
	if bindings == nil {
		return nil, fmt.Errorf("invalid storage token registry")
	}
	return bindings, nil
}

func (r *Receiver) check(ctx context.Context, folder, token string) error {
	checker, err := New([]Share{{Path: folder, ID: token}}, true)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	checker.slot = r.slot
	checker.boundary = r.Root
	return checker.Check(ctx)
}

func (r *Receiver) Verify(ctx context.Context, path, token string) error {
	if len(token) != 64 {
		return fmt.Errorf("%w: invalid shared token", ErrUnavailable)
	}
	for _, ch := range token {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return fmt.Errorf("%w: invalid shared token", ErrUnavailable)
		}
	}
	folder, err := r.Resolve(path)
	if err != nil {
		return err
	}
	if err := r.check(ctx, folder, token); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bindings, err := r.bindings()
	if err != nil {
		return err
	}
	if bindings[folder] == token {
		return nil
	}
	bindings[folder] = token
	data, err := json.Marshal(bindings)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(r.Registry), ".storage-tokens-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), r.Registry)
}

// CheckTarget uses the longest registered folder ancestor. Missing registration
// fails closed; finding an arbitrary token on disk is never enough.
func (r *Receiver) CheckTarget(ctx context.Context, path string, deleting bool) error {
	full, err := r.Resolve(path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	bindings, err := r.bindings()
	r.mu.Unlock()
	if err != nil {
		return err
	}
	var folder, token string
	for candidate, expected := range bindings {
		if !within(r.Root, candidate) {
			return fmt.Errorf("invalid registered folder")
		}
		if deleting && within(full, candidate) {
			return ErrProtected
		}
		if within(candidate, full) && len(candidate) > len(folder) {
			folder, token = candidate, expected
		}
	}
	if folder == "" {
		return fmt.Errorf("%w: folder token has not been verified by the sender", ErrUnavailable)
	}
	return r.check(ctx, folder, token)
}

func (r *Receiver) CheckKnown(ctx context.Context) error {
	bindings, err := r.bindings()
	if err != nil {
		return err
	}
	for folder, token := range bindings {
		if !within(r.Root, folder) {
			return fmt.Errorf("invalid registered folder")
		}
		if err := r.check(ctx, folder, token); err != nil {
			return err
		}
	}
	return nil
}

func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := r.Verify(req.Context(), req.URL.Query().Get("path"), req.Header.Get("X-Schnorarr-Shared-Token")); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable", "message": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
