package peer

import (
	"path/filepath"
	"testing"
)

func TestDerivePeerDir(t *testing.T) {
	tests := []struct {
		base string
		addr string
		sub  string
	}{
		{"data", "127.0.0.1:9001", "127-0-0-1-9001"},
		{"data", "[::1]:8000", "--1-8000"},
		{"", "peer", "peer-peer"},
	}
	for _, tt := range tests {
		base := tt.base
		if base == "" {
			base = "."
		}
		want := filepath.Join(base, tt.sub)
		if got := derivePeerDir(tt.base, tt.addr); got != want {
			t.Fatalf("derivePeerDir(%q,%q)=%q want %q", tt.base, tt.addr, got, want)
		}
	}
}

func TestSanitizePathToken(t *testing.T) {
	if got := sanitizePathToken("  bad/:path  "); got != "bad-path" {
		t.Fatalf("unexpected sanitize result: %s", got)
	}
	if got := sanitizePathToken("???"); got != "peer" {
		t.Fatalf("non alnum should fallback to peer, got %s", got)
	}
}

func TestChooseName(t *testing.T) {
	if got := chooseName("bob", "alice"); got != "alice" {
		t.Fatalf("expected resolved name to win")
	}
	if got := chooseName("bob", ""); got != "bob" {
		t.Fatalf("expected fallback to target")
	}
}
