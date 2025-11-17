package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"

	"golang.org/x/crypto/scrypt"
)

// Box handles symmetric encryption/decryption of payloads.
type Box struct {
	gcm cipher.AEAD
}

type envelope struct {
	Nonce string `json:"nonce"`
	Data  string `json:"data"`
}

// NewBox derives an AES-GCM box from a shared secret.
func NewBox(secret string) (*Box, error) {
	if secret == "" {
		return nil, nil
	}
	salt := sha256.Sum256([]byte(secret))
	key, err := scrypt.Key([]byte(secret), salt[:], 1<<15, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt serialises encrypted payload as JSON.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	if b == nil {
		return plaintext, nil
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := b.gcm.Seal(nil, nonce, plaintext, nil)
	env := envelope{
		Nonce: base64.StdEncoding.EncodeToString(nonce),
		Data:  base64.StdEncoding.EncodeToString(ciphertext),
	}
	return json.Marshal(env)
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(payload []byte) ([]byte, error) {
	if b == nil {
		return payload, nil
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, err
	}
	if len(nonce) != b.gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, err
	}
	return b.gcm.Open(nil, nonce, ciphertext, nil)
}
