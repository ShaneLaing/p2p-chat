package authutil

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	secretOnce sync.Once // Ensure that the key is only read and initialized once.
	secretKey  []byte
)

// getSecret retrieves the secret key from environment variable or defaults for development.
func getSecret() []byte {
	secretOnce.Do(func() {
		key := os.Getenv("P2P_AUTH_SECRET")
		if key == "" {
			key = "dev-secret-change-me" // using a default for development
		}
		secretKey = []byte(key)
	})
	return secretKey
}

// IssueToken returns a signed JWT for the provided username.
func IssueToken(username string) (string, error) {
	claims := jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims) // Create token with claims
	return token.SignedString(getSecret())
}

// ValidateToken parses token string and validates signature, returning username.
func ValidateToken(tokenStr string) (string, error) {
	if tokenStr == "" {
		return "", errors.New("empty token")
	}
	// check if token method is the HMAC and validate signature
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return getSecret(), nil
	})
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if username, ok := claims["username"].(string); ok {
			return username, nil
		}
	}
	return "", errors.New("invalid token claims")
}
