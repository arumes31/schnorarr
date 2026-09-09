package ui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestDashboardTerminalTextDoesNotFlicker(t *testing.T) {
	stylesheet, err := fs.ReadFile(StaticFS, "css/dashboard.css")
	if err != nil {
		t.Fatalf("read dashboard stylesheet: %v", err)
	}

	css := string(stylesheet)
	for _, flickerRule := range []string{"animation: flicker", "@keyframes flicker"} {
		if strings.Contains(css, flickerRule) {
			t.Errorf("dashboard stylesheet still contains %q", flickerRule)
		}
	}
}
