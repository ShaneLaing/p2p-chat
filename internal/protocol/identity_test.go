package protocol

import "testing"

func TestIdentitySetDisplay(t *testing.T) {
	id := NewIdentity("", "fallback")
	if got := id.Get(); got != "fallback" {
		t.Fatalf("expected fallback name, got %s", got)
	}
	if !id.SetDisplay("alice") {
		t.Fatalf("expected SetDisplay to report change")
	}
	if id.Get() != "alice" {
		t.Fatalf("expected name updated to alice")
	}
	if id.SetDisplay("alice") {
		t.Fatalf("setting same name should not report change")
	}
}

func TestIdentitySetAuth(t *testing.T) {
	id := NewIdentity("bob", "bob")
	if !id.SetAuth("alice", "token") {
		t.Fatalf("expected auth change to be reported")
	}
	if id.Get() != "alice" {
		t.Fatalf("expected name switched to auth user")
	}
	if id.Token() != "token" {
		t.Fatalf("expected token stored")
	}
	if id.SetAuth("", "") {
		t.Fatalf("empty auth should be ignored")
	}
}
