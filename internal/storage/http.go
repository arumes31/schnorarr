package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CheckRemote requires a fresh readiness response, not liveness from /health.
// Older receivers without the readiness endpoint fail closed.
func CheckRemote(ctx context.Context, endpoint string, token ...string) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout+2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(token) > 0 {
		req.Header.Set("X-Schnorarr-Shared-Token", token[0])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: receiver: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	var result struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: receiver readiness returned HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return fmt.Errorf("%w: receiver readiness: %w", ErrUnavailable, err)
	}
	if result.Status != "ready" {
		return fmt.Errorf("%w: receiver is not ready", ErrUnavailable)
	}
	return nil
}

func (c *Checker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := c.Check(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable", "message": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
