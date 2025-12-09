package protocol

import "testing"

func TestChooseName(t *testing.T) {
	if got := chooseName("bob", "alice"); got != "alice" {
		t.Fatalf("expected resolved name to win")
	}
	if got := chooseName("bob", ""); got != "bob" {
		t.Fatalf("expected fallback to target")
	}
}
