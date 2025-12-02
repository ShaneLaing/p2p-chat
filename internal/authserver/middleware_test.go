package authserver

import (
	"net/http/httptest"
	"testing"
)

func TestRoutePatternFallsBackToPath(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	if got := routePattern(req); got != "/healthz" {
		t.Fatalf("expected path fallback, got %s", got)
	}
}

func TestClientOriginPrefersForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	req.RemoteAddr = "2.2.2.2"
	if got := clientOrigin(req); got != "1.1.1.1" {
		t.Fatalf("expected forwarded header, got %s", got)
	}
}
