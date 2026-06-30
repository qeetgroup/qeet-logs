// Package security holds shared crypto helpers for the Go services.
package security

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey returns the hex-encoded SHA-256 of a raw API key. It must match the
// hash the Rust ingest gateway computes, so api_keys.key_hash resolves the same
// tenant on both the ingest and query planes.
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
