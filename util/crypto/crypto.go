// Package crypto provides cryptographic utilities for password hashing and verification.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// HashPasswordAsBcrypt generates a bcrypt hash of the given password.
func HashPasswordAsBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// CheckPasswordHash verifies if the given password matches the bcrypt hash.
func CheckPasswordHash(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// encPrefix marks a string as already encrypted by EncryptString. Both
// hooks and round-trip helpers check this prefix so encrypting an already
// encrypted value (or decrypting a legacy plaintext one) is a no-op.
const encPrefix = "enc:v1:"

var (
	keyMu     sync.RWMutex
	cachedKey []byte
	keyPathFn = defaultKeyPath
)

// SetEncryptionKeyPath overrides where the AES key file lives.
// Defaults to <db_folder>/encryption.key alongside the SQLite database.
// Tests use this to point at a temp dir.
func SetEncryptionKeyPath(path string) {
	keyMu.Lock()
	defer keyMu.Unlock()
	keyPathFn = func() (string, error) { return path, nil }
	cachedKey = nil // force re-read with new path
}

// defaultKeyPath returns <XUI_DB_FOLDER>/encryption.key when XUI_DB_FOLDER is
// set, otherwise falls back to /etc/x-ui (matching the panel default).
func defaultKeyPath() (string, error) {
	dir := os.Getenv("XUI_DB_FOLDER")
	if dir == "" {
		dir = "/etc/x-ui"
	}
	return filepath.Join(dir, "encryption.key"), nil
}

// loadOrGenerateKey returns the 32-byte AES key, creating the file with
// fresh random bytes on first call. Subsequent calls reuse the in-memory cache.
func loadOrGenerateKey() ([]byte, error) {
	keyMu.RLock()
	if cachedKey != nil {
		k := cachedKey
		keyMu.RUnlock()
		return k, nil
	}
	keyMu.RUnlock()

	keyMu.Lock()
	defer keyMu.Unlock()
	if cachedKey != nil {
		return cachedKey, nil
	}
	path, err := keyPathFn()
	if err != nil {
		return nil, err
	}

	// fast path: file exists
	if data, err := os.ReadFile(path); err == nil {
		// allow base64 (used for /etc-friendly storage) or raw bytes
		if dec, derr := base64.StdEncoding.DecodeString(string(data)); derr == nil && len(dec) == 32 {
			cachedKey = dec
			return cachedKey, nil
		}
		if len(data) == 32 {
			cachedKey = data
			return cachedKey, nil
		}
		return nil, fmt.Errorf("encryption key file %s exists but is malformed", path)
	}

	// generate new key
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir for encryption key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write encryption key: %w", err)
	}
	cachedKey = key
	return cachedKey, nil
}

// IsEncrypted reports whether s carries the enc:v1: marker.
func IsEncrypted(s string) bool {
	return len(s) > len(encPrefix) && s[:len(encPrefix)] == encPrefix
}

// EncryptString returns s encrypted with AES-256-GCM, prefixed with "enc:v1:".
// Empty strings pass through unchanged so empty fields stay empty in the DB.
// Already-encrypted strings (carrying the prefix) are returned as-is — this
// makes the function idempotent and safe to call from BeforeSave hooks.
func EncryptString(s string) (string, error) {
	if s == "" || IsEncrypted(s) {
		return s, nil
	}
	key, err := loadOrGenerateKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(s), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptString reverses EncryptString. Strings without the enc:v1: prefix
// are returned as-is — that's how legacy plaintext rows survive the migration.
func DecryptString(s string) (string, error) {
	if !IsEncrypted(s) {
		return s, nil
	}
	payload, err := base64.StdEncoding.DecodeString(s[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("decrypt: bad base64: %w", err)
	}
	key, err := loadOrGenerateKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("decrypt: payload too short")
	}
	nonce := payload[:gcm.NonceSize()]
	ct := payload[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}
