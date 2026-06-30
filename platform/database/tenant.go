package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKey is the result of resolving a raw API key to its owning tenant + scopes.
type APIKey struct {
	TenantID string
	Scopes   []string
}

// LookupAPIKey resolves a SHA-256 API-key hash to its tenant and granted scopes.
// Returns (_, false, nil) when no active (unrevoked, unexpired) key matches.
// Runs without a tenant RLS context — it is the auth root, like the tenants table.
func LookupAPIKey(ctx context.Context, pool *pgxpool.Pool, hash string) (APIKey, bool, error) {
	var k APIKey
	err := pool.QueryRow(ctx,
		`SELECT tenant_id::text, scopes
		   FROM api_keys
		  WHERE key_hash = $1
		    AND revoked_at IS NULL
		    AND (expires_at IS NULL OR expires_at > now())
		  LIMIT 1`,
		hash,
	).Scan(&k.TenantID, &k.Scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, false, nil
	}
	if err != nil {
		return APIKey{}, false, fmt.Errorf("api key lookup: %w", err)
	}
	return k, true, nil
}

// WithTenant runs fn inside a transaction with the RLS context set to tenantID.
// Domain tables that carry a tenant_id RLS policy are automatically scoped.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, TRUE)`, tenantID,
	); err != nil {
		return fmt.Errorf("set tenant_id: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
