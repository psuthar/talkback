package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// EncryptToken encrypts plaintext with AES-256-GCM using key derived from ENCRYPTION_KEY env
func EncryptToken(plaintext []byte, keyB64 string) ([]byte, error) {
	key, err := deriveKey(keyB64)
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
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptToken decrypts ciphertext encrypted with EncryptToken
func DecryptToken(ciphertext []byte, keyB64 string) ([]byte, error) {
	key, err := deriveKey(keyB64)
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
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func deriveKey(keyB64 string) ([]byte, error) {
	if keyB64 == "" {
		return nil, errors.New("ENCRYPTION_KEY is not set")
	}
	decoded, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		// If not base64, derive 32 bytes from the string
		h := sha256.Sum256([]byte(keyB64))
		return h[:], nil
	}
	if len(decoded) != 32 {
		h := sha256.Sum256(decoded)
		return h[:], nil
	}
	return decoded, nil
}
