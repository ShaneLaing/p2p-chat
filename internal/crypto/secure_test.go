package crypto

import "testing"

func TestBoxEncryptDecrypt(t *testing.T) {
	box, err := NewBox("secret")
	if err != nil {
		t.Fatalf("NewBox error: %v", err)
	}
	plaintext := []byte("hello world")
	cipher, err := box.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	decoded, err := box.Decrypt(cipher)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if string(decoded) != string(plaintext) {
		t.Fatalf("round trip mismatch")
	}
}

func TestBoxNilSecretPassthrough(t *testing.T) {
	box, err := NewBox("")
	if err != nil {
		t.Fatalf("NewBox with empty secret should not error: %v", err)
	}
	data := []byte("hola")
	cipher, err := box.Encrypt(data)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	if string(cipher) != string(data) {
		t.Fatalf("expected passthrough for nil box")
	}
}

func TestBoxRejectsTamperedPayload(t *testing.T) {
	box, err := NewBox("secret")
	if err != nil {
		t.Fatalf("NewBox error: %v", err)
	}
	cipher, err := box.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	cipher[len(cipher)-1] ^= 0x01
	if _, err := box.Decrypt(cipher); err == nil {
		t.Fatalf("expected decrypt error for tampered payload")
	}
}
