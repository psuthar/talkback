package invitations

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/base64"
)

const tokenByteSize = 32

// GenerateToken returns a URL-safe random token. Caller must never log or store this; only hash and store.
func GenerateToken() (raw string, hash string, err error) {
	b := make([]byte, tokenByteSize)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 hex hash of the raw token. Use for storage and lookup.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// TokenMatches returns true if raw hashes to the given hash (constant-time comparison via hex decode then subtle).
func TokenMatches(raw, storedHash string) bool {
	computed := HashToken(raw)
	return subtleEqual(computed, storedHash)
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
