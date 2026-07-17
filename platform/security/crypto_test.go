package security

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func newTestKey(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	t.Setenv("QEET_LOGS_SECRETS_KEY", newTestKey(t))
	if !SecretsConfigured() {
		t.Fatal("expected configured with a valid key")
	}
	const secret = "xoxb-slack-bot-token-1234567890"
	enc, err := EncryptSecret(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(enc, "v1:") {
		t.Errorf("ciphertext should be versioned, got %q", enc)
	}
	if strings.Contains(enc, secret) {
		t.Error("ciphertext must not contain the plaintext")
	}
	dec, err := DecryptSecret(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != secret {
		t.Errorf("roundtrip = %q, want %q", dec, secret)
	}
}

func TestEncryptTwiceDiffersButBothDecrypt(t *testing.T) {
	t.Setenv("QEET_LOGS_SECRETS_KEY", newTestKey(t))
	a, _ := EncryptSecret("same-secret")
	b, _ := EncryptSecret("same-secret")
	if a == b {
		t.Error("nonce should make repeated encryptions differ")
	}
	da, _ := DecryptSecret(a)
	db, _ := DecryptSecret(b)
	if da != "same-secret" || db != "same-secret" {
		t.Errorf("both must decrypt to the same plaintext, got %q / %q", da, db)
	}
}

func TestDevPassthroughWithoutKey(t *testing.T) {
	t.Setenv("QEET_LOGS_SECRETS_KEY", "")
	if SecretsConfigured() {
		t.Fatal("must not be configured with an empty key")
	}
	enc, err := EncryptSecret("plain-token")
	if err != nil {
		t.Fatalf("passthrough encrypt: %v", err)
	}
	if enc != "plain-token" {
		t.Errorf("keyless encrypt must pass through unchanged, got %q", enc)
	}
	// Legacy/plaintext value decrypts to itself (no prefix).
	dec, err := DecryptSecret("plain-token")
	if err != nil || dec != "plain-token" {
		t.Errorf("unprefixed decrypt = %q,%v; want plain-token,nil", dec, err)
	}
}

func TestEmptyStaysEmpty(t *testing.T) {
	t.Setenv("QEET_LOGS_SECRETS_KEY", newTestKey(t))
	if got, _ := EncryptSecret(""); got != "" {
		t.Errorf("empty encrypt = %q, want empty", got)
	}
}

func TestInvalidKeyRejected(t *testing.T) {
	t.Setenv("QEET_LOGS_SECRETS_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
	if SecretsConfigured() {
		t.Error("a non-32-byte key must be rejected")
	}
	if _, err := EncryptSecret("x"); err == nil {
		t.Error("expected error encrypting with an invalid key")
	}
}
