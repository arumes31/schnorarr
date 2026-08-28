package internalapi

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const minTokenBytes = 32

// Token returns the dedicated sender-to-receiver credential. It is separate
// from browser sessions and must never be sent in a URL.
func Token(getenv func(string) string) (string, error) {
	token := strings.TrimSpace(getenv("INTERNAL_API_TOKEN"))
	if len(token) < minTokenBytes {
		return "", fmt.Errorf("INTERNAL_API_TOKEN must contain at least %d bytes", minTokenBytes)
	}
	return token, nil
}

// RequireToken protects the receiver's machine API with constant-time bearer
// token comparison.
func RequireToken(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ReceiverURL constructs an HTTPS endpoint from the deployment-controlled
// receiver origin. Paths and query parameters are supplied by the caller and
// cannot alter the configured authority.
func ReceiverURL(getenv func(string) string, endpoint string, query url.Values) (string, error) {
	raw := strings.TrimSpace(getenv("RECEIVER_API_URL"))
	base, err := url.Parse(raw)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return "", errors.New("RECEIVER_API_URL must be an https origin")
	}
	if base.User != nil || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("RECEIVER_API_URL must contain only scheme and authority")
	}
	endpointURL := *base
	endpointURL.Path = endpoint
	endpointURL.RawQuery = query.Encode()
	return endpointURL.String(), nil
}

// NewClient creates a redirect-denying TLS client. A private CA can be mounted
// through INTERNAL_API_CA_FILE; certificate verification is never disabled.
func NewClient(getenv func(string) string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile := strings.TrimSpace(getenv("INTERNAL_API_CA_FILE")); caFile != "" {
		// #nosec G304 -- this is an operator-mounted CA path read once at startup, not request input.
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading internal API CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("INTERNAL_API_CA_FILE contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// NewRequest creates an authenticated request to the configured receiver.
func NewRequest(ctx context.Context, getenv func(string) string, method, endpoint string, query url.Values) (*http.Request, error) {
	token, err := Token(getenv)
	if err != nil {
		return nil, err
	}
	endpointURL, err := ReceiverURL(getenv, endpoint, query)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpointURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return request, nil
}
