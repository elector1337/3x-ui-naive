package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	SetEncryptionKeyPath(filepath.Join(dir, "encryption.key"))

	cases := []string{
		"",
		"hello",
		"S3CrEt!@#",
		"русский пароль с пробелом",
		"a long string that contains lots of characters, more than one nonce-block worth of data, just to make sure",
	}
	for _, plain := range cases {
		t.Run(plain[:min(len(plain), 12)], func(t *testing.T) {
			ct, err := EncryptString(plain)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			if plain == "" {
				if ct != "" {
					t.Errorf("empty input should pass through, got %q", ct)
				}
				return
			}
			if !IsEncrypted(ct) {
				t.Errorf("output missing enc:v1: prefix: %q", ct)
			}
			if ct == plain {
				t.Errorf("ciphertext == plaintext: %q", ct)
			}
			pt, err := DecryptString(ct)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}
			if pt != plain {
				t.Errorf("roundtrip mismatch: %q vs %q", pt, plain)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestEncryptString_Idempotent(t *testing.T) {
	dir := t.TempDir()
	SetEncryptionKeyPath(filepath.Join(dir, "encryption.key"))

	ct1, err := EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := EncryptString(ct1)
	if err != nil {
		t.Fatal(err)
	}
	if ct1 != ct2 {
		t.Errorf("encrypting already-encrypted value should be a no-op, got %q vs %q", ct1, ct2)
	}
}

func TestDecryptString_PassthroughForLegacyPlaintext(t *testing.T) {
	dir := t.TempDir()
	SetEncryptionKeyPath(filepath.Join(dir, "encryption.key"))

	// legacy rows in the DB still contain plaintext — DecryptString must
	// return them unchanged so migration is automatic
	got, err := DecryptString("legacy_plaintext_password")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy_plaintext_password" {
		t.Errorf("legacy plaintext should pass through, got %q", got)
	}
}

func TestKeyFile_PersistsAcrossLoads(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")
	SetEncryptionKeyPath(keyPath)

	ct, err := EncryptString("hello")
	if err != nil {
		t.Fatal(err)
	}
	// reset cache to force re-read from disk
	keyMu.Lock()
	cachedKey = nil
	keyMu.Unlock()

	pt, err := DecryptString(ct)
	if err != nil {
		t.Fatalf("decrypt after reload: %v", err)
	}
	if pt != "hello" {
		t.Errorf("got %q want hello", pt)
	}
	// verify file is 0600
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file perms %v, want 0o600", info.Mode().Perm())
	}
}

func TestDecryptString_DifferentKeyFails(t *testing.T) {
	dir := t.TempDir()
	SetEncryptionKeyPath(filepath.Join(dir, "encryption.key"))

	ct, err := EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}

	// switch to a different key
	dir2 := t.TempDir()
	SetEncryptionKeyPath(filepath.Join(dir2, "encryption.key"))

	if _, err := DecryptString(ct); err == nil {
		t.Errorf("expected decryption to fail with different key")
	}
}
