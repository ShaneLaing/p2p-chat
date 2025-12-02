package authutil

import "testing"

func TestIssueAndValidateToken(t *testing.T) {
	token, err := IssueToken("alice")
	if err != nil {
		t.Fatalf("IssueToken error: %v", err)
	}
	username, err := ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected username alice, got %s", username)
	}
}

func TestValidateTokenRejectsInvalid(t *testing.T) {
	if _, err := ValidateToken(""); err == nil {
		t.Fatalf("expected error for empty token")
	}
	token, err := IssueToken("bob")
	if err != nil {
		t.Fatalf("IssueToken error: %v", err)
	}
	tampered := token + "x"
	if _, err := ValidateToken(tampered); err == nil {
		t.Fatalf("expected error for tampered token")
	}
}
