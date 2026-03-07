package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const acceptTokenValidityMinutes = 10

// IssueAcceptToken returns a short-lived token that can be used once to authenticate
// for POST /api/invitations/accept (e.g. when the session cookie is not sent in incognito).
func IssueAcceptToken(userID uuid.UUID) (string, error) {
	secret := getAcceptTokenSecret()
	if secret == "" {
		return "", errors.New("accept token secret not configured (set ACCEPT_TOKEN_SECRET or ENCRYPTION_KEY)")
	}
	expiry := time.Now().Add(acceptTokenValidityMinutes * time.Minute).Unix()
	b := make([]byte, 0, 16+8)
	b = append(b, userID[:]...)
	b = append(b, make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[16:], uint64(expiry))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(b)
	sig := mac.Sum(nil)
	payload := append(b, sig...)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(payload), nil
}

// ValidateAcceptToken parses and validates the token; returns the user ID or an error.
func ValidateAcceptToken(token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, errors.New("token empty")
	}
	secret := getAcceptTokenSecret()
	if secret == "" {
		return uuid.Nil, errors.New("accept token secret not configured")
	}
	payload, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(strings.TrimSpace(token))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid token: %w", err)
	}
	// 16 (uuid) + 8 (expiry) + 32 (sha256) = 56
	if len(payload) < 56 {
		return uuid.Nil, errors.New("invalid token length")
	}
	b, sig := payload[:24], payload[24:56]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(b)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return uuid.Nil, errors.New("invalid token signature")
	}
	expiry := int64(binary.BigEndian.Uint64(b[16:24]))
	if time.Now().Unix() > expiry {
		return uuid.Nil, errors.New("token expired")
	}
	var id uuid.UUID
	copy(id[:], b[:16])
	return id, nil
}

func getAcceptTokenSecret() string {
	if s := os.Getenv("ACCEPT_TOKEN_SECRET"); s != "" {
		return s
	}
	return os.Getenv("ENCRYPTION_KEY")
}
