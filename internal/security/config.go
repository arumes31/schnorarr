package security

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"schnorarr/internal/internalapi"
)

const minAdminPassword = 16
const minRsyncPassword = 16

var knownDefaults = map[string]struct{}{
	"admin":          {},
	"changeme":       {},
	"password":       {},
	"schnorarr":      {},
	"secretpassword": {},
}

// ValidateEnvironment rejects source-known credentials and insecure network
// configuration before any listener or background worker starts.
func ValidateEnvironment(getenv func(string) string) error {
	mode := strings.TrimSpace(getenv("MODE"))
	if mode != "sender" && mode != "receiver" {
		return errors.New("MODE must be sender or receiver")
	}
	if _, err := internalapi.Token(getenv); err != nil {
		return err
	}
	if strings.TrimSpace(getenv("RSYNC_USER")) == "" {
		return errors.New("RSYNC_USER is required")
	}
	rsyncPassword := getenv("RSYNC_PASSWORD")
	if utf8.RuneCountInString(rsyncPassword) < minRsyncPassword {
		return fmt.Errorf("RSYNC_PASSWORD must contain at least %d characters", minRsyncPassword)
	}
	if strings.ContainsAny(rsyncPassword, "\r\n") {
		return errors.New("RSYNC_PASSWORD must not contain line breaks")
	}
	if _, insecure := knownDefaults[strings.ToLower(rsyncPassword)]; insecure {
		return errors.New("RSYNC_PASSWORD uses a known insecure value")
	}
	if strings.TrimSpace(getenv("AUTH_ENABLED")) != "true" {
		return errors.New("AUTH_ENABLED must be true")
	}
	user := strings.TrimSpace(getenv("ADMIN_USER"))
	if user == "" {
		return errors.New("ADMIN_USER is required")
	}
	password := getenv("ADMIN_PASS")
	if utf8.RuneCountInString(password) < minAdminPassword {
		return fmt.Errorf("ADMIN_PASS must contain at least %d characters", minAdminPassword)
	}
	if _, insecure := knownDefaults[strings.ToLower(password)]; insecure {
		return errors.New("ADMIN_PASS uses a known insecure value")
	}
	if strings.EqualFold(user, password) {
		return errors.New("ADMIN_PASS must differ from ADMIN_USER")
	}

	certFile := strings.TrimSpace(getenv("TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(getenv("TLS_KEY_FILE"))
	if certFile == "" || keyFile == "" {
		return errors.New("TLS_CERT_FILE and TLS_KEY_FILE are required")
	}
	for name, path := range map[string]string{"TLS_CERT_FILE": certFile, "TLS_KEY_FILE": keyFile} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s must reference a readable regular file", name)
		}
	}

	if mode == "sender" {
		raw := strings.TrimSpace(getenv("RECEIVER_API_URL"))
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("RECEIVER_API_URL must be an https origin in sender mode")
		}
	}
	return nil
}
