package database

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qeetgroup/qeet-logs-server/platform/security"
)

// NewAPIKey is the result of creating a scoped API key. The raw Key is returned
// exactly once; only its SHA-256 hash persists in the database.
type NewAPIKey struct {
	ID        string
	TenantID  string
	Name      string
	Key       string // raw key — show once, never store
	KeyPrefix string
	Scopes    []string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// APIKeyRow is a safe projection of api_keys (raw key omitted).
type APIKeyRow struct {
	ID         string
	TenantID   string
	Name       string
	KeyPrefix  string
	Scopes     []string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// CreateAPIKey generates a new scoped API key for tenantID. The raw key is
// returned in NewAPIKey.Key and is never stored — callers must display it
// immediately and discard.
func CreateAPIKey(ctx context.Context, pool *pgxpool.Pool, tenantID, name string, scopes []string, expiresAt *time.Time) (NewAPIKey, error) {
	raw, err := generateKey()
	if err != nil {
		return NewAPIKey{}, fmt.Errorf("generate key: %w", err)
	}
	hash := security.HashAPIKey(raw)
	prefix := raw[:min(12, len(raw))]

	var nk NewAPIKey
	nk.Key = raw
	nk.KeyPrefix = prefix

	err = pool.QueryRow(ctx,
		`INSERT INTO api_keys (tenant_id, name, key_hash, key_prefix, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id::text, tenant_id::text, name, key_prefix, scopes, expires_at, created_at`,
		tenantID, name, hash, prefix, scopes, expiresAt,
	).Scan(&nk.ID, &nk.TenantID, &nk.Name, &nk.KeyPrefix, &nk.Scopes, &nk.ExpiresAt, &nk.CreatedAt)
	if err != nil {
		return NewAPIKey{}, fmt.Errorf("insert api key: %w", err)
	}
	return nk, nil
}

// ListAPIKeys returns all active (non-revoked) API keys for tenantID.
func ListAPIKeys(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]APIKeyRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, tenant_id::text, name, key_prefix, scopes,
		        last_used_at, expires_at, revoked_at, created_at
		   FROM api_keys
		  WHERE tenant_id = $1::uuid
		    AND revoked_at IS NULL
		  ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var out []APIKeyRow
	for rows.Next() {
		var k APIKeyRow
		if err := rows.Scan(
			&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.Scopes,
			&k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey soft-deletes an API key by setting revoked_at to now. It only
// operates on keys belonging to tenantID (prevents cross-tenant revocation).
// Returns (false, nil) when no matching active key was found.
func RevokeAPIKey(ctx context.Context, pool *pgxpool.Pool, tenantID, keyID string) (bool, error) {
	tag, err := pool.Exec(ctx,
		`UPDATE api_keys
		    SET revoked_at = now()
		  WHERE id = $1::uuid
		    AND tenant_id = $2::uuid
		    AND revoked_at IS NULL`,
		keyID, tenantID,
	)
	if err != nil {
		return false, fmt.Errorf("revoke api key: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// TouchLastUsed updates the last_used_at timestamp for the key identified by
// its hash. Best-effort — called asynchronously in the auth middleware.
func TouchLastUsed(ctx context.Context, pool *pgxpool.Pool, hash string) {
	pool.Exec(ctx, //nolint:errcheck
		`UPDATE api_keys SET last_used_at = now() WHERE key_hash = $1`, hash)
}

// generateKey returns a URL-safe random key prefixed with "qeel_".
func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "qeel_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
