package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Envelope encryption for secrets stored at rest (product-readiness; e.g. the
// Slack bot_token in chatops_installations). AES-256-GCM with a key supplied via
// QEET_LOGS_SECRETS_KEY (base64 of exactly 32 bytes). Ciphertext is versioned
// ("v1:" + base64(nonce||ciphertext||tag)) so the scheme can evolve and so
// Decrypt can distinguish encrypted values from legacy plaintext.
//
// Dev passthrough: when QEET_LOGS_SECRETS_KEY is unset, EncryptSecret returns the
// plaintext UNCHANGED (no fake "v1:" prefix), and DecryptSecret returns any
// unprefixed value unchanged. This keeps local dev keyless and is honest — a
// value is only encrypted when a key actually exists. In production the key is
// required (SecretsConfigured() == true) and every new secret is encrypted.

const secretVersionPrefix = "v1:"

// errNoKey signals that encryption was requested without a configured key.
var errNoKey = errors.New("QEET_LOGS_SECRETS_KEY not set")

// SecretsConfigured reports whether a valid 32-byte base64 key is present.
func SecretsConfigured() bool {
	_, err := loadSecretsKey()
	return err == nil
}

// loadSecretsKey decodes QEET_LOGS_SECRETS_KEY into a 32-byte AES key.
func loadSecretsKey() ([]byte, error) {
	raw := os.Getenv("QEET_LOGS_SECRETS_KEY")
	if raw == "" {
		return nil, errNoKey
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("QEET_LOGS_SECRETS_KEY is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("QEET_LOGS_SECRETS_KEY must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// EncryptSecret encrypts plaintext with AES-256-GCM. When no key is configured
// it returns the plaintext unchanged (dev passthrough) so callers need no
// branching. An empty string is returned unchanged.
func EncryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := loadSecretsKey()
	if err != nil {
		if errors.Is(err, errNoKey) {
			return plaintext, nil // dev passthrough
		}
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return secretVersionPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret reverses EncryptSecret. Values without the version prefix are
// treated as legacy plaintext and returned unchanged (so dev-stored or
// pre-encryption rows keep working). A prefixed value with no/invalid key errors.
func DecryptSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, secretVersionPrefix) {
		return stored, nil // legacy plaintext / dev passthrough
	}
	key, err := loadSecretsKey()
	if err != nil {
		return "", fmt.Errorf("cannot decrypt secret: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, secretVersionPrefix))
	if err != nil {
		return "", fmt.Errorf("secret is not valid base64: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("secret ciphertext too short")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secret decryption failed: %w", err)
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
