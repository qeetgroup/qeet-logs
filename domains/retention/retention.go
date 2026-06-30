// Package retention reads per-tenant retention + PII-masking configuration
// (Modules 3.2, 10.2). The ingest pipeline (M2) uses it to stamp each record's
// _retention_days and to drive the PII gate's masking actions; ClickHouse's
// per-record TTL then enforces hard deletion at the boundary (Module 3.3).
package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultDays is the fallback retention window when a tenant has no explicit config.
const DefaultDays = 7

// Config is a tenant's effective retention + masking configuration.
type Config struct {
	RetentionDays int
	// MaskingActions maps a PII type (email|ip|jwt|card|phone|national_id|...)
	// to an action (mask|hash|drop_field|drop_record).
	MaskingActions map[string]string
}

// Get returns the retention config for a tenant, falling back to defaults when
// no retention_config row exists yet.
func Get(ctx context.Context, pool *pgxpool.Pool, tenantID string) (Config, error) {
	cfg := Config{RetentionDays: DefaultDays, MaskingActions: map[string]string{}}

	var actions []byte
	err := pool.QueryRow(ctx,
		`SELECT retention_days, masking_actions FROM retention_config WHERE tenant_id = $1`,
		tenantID,
	).Scan(&cfg.RetentionDays, &actions)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read retention config: %w", err)
	}
	if len(actions) > 0 {
		if err := json.Unmarshal(actions, &cfg.MaskingActions); err != nil {
			return Config{}, fmt.Errorf("decode masking_actions: %w", err)
		}
	}
	return cfg, nil
}
