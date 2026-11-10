package handlers

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The shipped image must not opt out of dashboard authentication or bake in a
// well-known password; both defaults belong to the operator.
func TestDockerfileKeepsAuthDefaults(t *testing.T) {
	data, err := os.ReadFile("../../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			code.WriteString(line)
			code.WriteString("\n")
		}
	}
	if regexp.MustCompile(`AUTH_ENABLED\s*=\s*"?false`).MatchString(code.String()) {
		t.Error("Dockerfile must not set AUTH_ENABLED=false")
	}
	if regexp.MustCompile(`ADMIN_PASS\s*=`).MatchString(code.String()) {
		t.Error("Dockerfile must not bake in ADMIN_PASS")
	}
}
