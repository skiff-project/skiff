package server

import (
	"strings"
	"testing"
)

func TestUICSP(t *testing.T) {
	c := uiCSP("127.0.0.1:8484")
	for _, want := range []string{"script-src 'self'", "connect-src 'self' ws://127.0.0.1:8484 wss://127.0.0.1:8484", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(c, want) {
			t.Errorf("CSP lacks %q: %s", want, c)
		}
	}
	// a Host made to add directives is left out
	if c := uiCSP("evil.example; script-src *"); strings.Contains(c, "evil") || strings.Contains(c, "script-src *") {
		t.Errorf("injected: %s", c)
	}
}
