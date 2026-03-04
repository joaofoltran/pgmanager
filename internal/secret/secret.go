package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const prefix = "enc:v1:"

// Box provides AES-256-GCM encryption/decryption for secrets at rest.
// The key is derived by SHA-256 hashing the provided passphrase,
// ensuring a fixed 32-byte key regardless of input length.
type Box struct {
	gcm cipher.AEAD
}

// NewBox creates a Box from a passphrase. The passphrase is hashed
// with SHA-256 to derive the AES-256 key.
func NewBox(passphrase string) (*Box, error) {
	if passphrase == "" {
		return nil, errors.New("encryption passphrase must not be empty")
	}
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt encrypts plaintext and returns a prefixed base64 string.
// Empty input returns empty output (nothing to protect).
func (b *Box) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ciphertext := b.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a value produced by Encrypt.
// If the value does not carry the "enc:v1:" prefix it is returned as-is
// (legacy plaintext — caller should re-encrypt).
func (b *Box) Decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, prefix) {
		return value, nil
	}
	data, err := base64.StdEncoding.DecodeString(value[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	nonceSize := b.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	plaintext, err := b.gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// IsEncrypted returns true if value carries the encryption prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, prefix)
}
