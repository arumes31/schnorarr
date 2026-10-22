package sync

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"time"
)

// EngineAPIHeader carries the shared secret on sender -> receiver engine API calls
// (/api/manifest, /api/stat, /api/delete).
const EngineAPIHeader = "X-Schnorarr-Engine-Key"

// engineHTTPClient is used for engine API calls without their own deadline.
var engineHTTPClient = &http.Client{Timeout: 2 * time.Minute}

// EngineAPIKey returns the shared engine API secret. ENGINE_API_KEY wins;
// otherwise RSYNC_PASSWORD is used, which both sides already share.
func EngineAPIKey() string {
	if key := os.Getenv("ENGINE_API_KEY"); key != "" {
		return key
	}
	return os.Getenv("RSYNC_PASSWORD")
}

// setEngineAPIKey attaches the shared secret to an outgoing engine API request.
func setEngineAPIKey(req *http.Request) {
	if key := EngineAPIKey(); key != "" {
		req.Header.Set(EngineAPIHeader, key)
	}
}

// ValidEngineAPIKey reports whether the request carries the configured secret.
// It fails closed when no secret is configured.
func ValidEngineAPIKey(r *http.Request) bool {
	want := EngineAPIKey()
	if want == "" {
		return false
	}
	got := sha256.Sum256([]byte(r.Header.Get(EngineAPIHeader)))
	exp := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(got[:], exp[:]) == 1
}
