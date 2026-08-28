package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateEnvironment(t *testing.T) {
	directory := t.TempDir()
	cert := filepath.Join(directory, "tls.crt")
	key := filepath.Join(directory, "tls.key")
	if err := os.WriteFile(cert, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"MODE":               "sender",
		"AUTH_ENABLED":       "true",
		"ADMIN_USER":         "operator",
		"ADMIN_PASS":         "a-strong-password-123",
		"INTERNAL_API_TOKEN": strings.Repeat("a", 32),
		"RSYNC_USER":         "syncuser",
		"RSYNC_PASSWORD":     "a-strong-rsync-password-123",
		"TLS_CERT_FILE":      cert,
		"TLS_KEY_FILE":       key,
		"RECEIVER_API_URL":   "https://receiver:8080",
	}
	getenv := func(key string) string { return values[key] }
	if err := ValidateEnvironment(getenv); err != nil {
		t.Fatalf("ValidateEnvironment() error = %v", err)
	}

	values["ADMIN_PASS"] = "schnorarr"
	if err := ValidateEnvironment(getenv); err == nil {
		t.Fatal("ValidateEnvironment() accepted a source-known password")
	}
	values["ADMIN_PASS"] = "a-strong-password-123"
	values["RSYNC_PASSWORD"] = "secretpassword"
	if err := ValidateEnvironment(getenv); err == nil {
		t.Fatal("ValidateEnvironment() accepted a source-known rsync password")
	}
	values["RSYNC_PASSWORD"] = "a-strong-rsync-password-123"
	values["RECEIVER_API_URL"] = "http://receiver:8080"
	if err := ValidateEnvironment(getenv); err == nil {
		t.Fatal("ValidateEnvironment() accepted plaintext receiver HTTP")
	}
}
