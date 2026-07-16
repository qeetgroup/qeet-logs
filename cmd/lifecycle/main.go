// Command lifecycle is the qeet-logs cold-tier storage mover (PRD Module 6 /
// P2-G2). It runs as a standalone long-running process alongside cmd/query and
// cmd/alerter. On an interval it snapshots ClickHouse `system.parts`, reads each
// tenant's tier configuration from Postgres (tenant_tiers), asks the pure
// domains/lifecycle planner which aged partitions belong on cold storage, and
// issues `ALTER TABLE … MOVE PARTITION … TO VOLUME 'cold'` for each.
//
// Deletion at the retention boundary is NOT this process's job — the per-record
// ClickHouse delete TTL (clickhouse/migrations/0009_cold_tier.sql) owns that.
// This mover only relocates hot→cold ahead of that delete, refining the global
// table-level move TTL with per-tenant hot windows.
//
// It requires a reachable ClickHouse cluster + S3/MinIO to do anything (`make
// infra-up`); with no infra it will fail the initial ping and exit, exactly like
// cmd/alerter.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qeetgroup/qeet-logs/domains/lifecycle"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs/platform/config"
	"github.com/qeetgroup/qeet-logs/platform/database"
	"github.com/qeetgroup/qeet-logs/platform/observability"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

var version = "dev"

// tables the mover tiers, matching clickhouse/migrations/0009_cold_tier.sql.
var trackedTables = []string{"logs", "metrics", "traces"}

// defaultTier is used for tenants with no tenant_tiers row (all-hot for 3 days,
// then cold, delete at 30d), matching the migration's global floor.
var defaultTier = lifecycle.TenantTier{HotDays: 3, ColdDays: 30}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	log := observability.New(cfg.Env)
	log.Info().Str("version", version).Msg("qeet-logs lifecycle mover starting")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer pool.Close()

	ch := clickhouse.New(cfg.ClickHouseURL, cfg.ClickHouseDatabase, cfg.ClickHouseUser, cfg.ClickHousePassword)
	if err := ch.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("clickhouse ping")
	}

	// Interval is generous — tiering is not latency-sensitive; TTL moves happen
	// during merges regardless, this just refines them per tenant.
	interval := 6 * time.Hour
	if v := os.Getenv("LIFECYCLE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runOnce(ctx, pool, ch, log) // eager first pass
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("lifecycle mover shutting down")
			return
		case <-ticker.C:
			runOnce(ctx, pool, ch, log)
		}
	}
}

// runOnce performs one snapshot → plan → move cycle. Best-effort: a failure on
// one partition or query is logged and skipped; it never takes the process down.
func runOnce(ctx context.Context, pool *pgxpool.Pool, ch *clickhouse.Client, log zerolog.Logger) {
	tiers, err := loadTiers(ctx, pool)
	if err != nil {
		log.Error().Err(err).Msg("load tenant tiers")
		return
	}
	parts, err := snapshotParts(ctx, ch)
	if err != nil {
		log.Error().Err(err).Msg("snapshot clickhouse parts")
		return
	}

	plan := lifecycle.MovePlan(parts, tiers, defaultTier)
	log.Info().Int("parts", len(parts)).Int("tenants", len(tiers)).Int("moves", len(plan)).Msg("lifecycle plan")

	moved := 0
	for _, m := range plan {
		// partition id is quoted; ClickHouse MOVE PARTITION ID '<id>' relocates it.
		stmt := fmt.Sprintf(
			"ALTER TABLE qeet_logs.%s MOVE PARTITION ID '%s' TO VOLUME 'cold'",
			m.Table, escapeID(m.Partition))
		if err := ch.Exec(ctx, stmt); err != nil {
			log.Error().Err(err).Str("table", m.Table).Str("partition", m.Partition).Msg("move partition")
			continue
		}
		moved++
		log.Debug().Str("table", m.Table).Str("partition", m.Partition).
			Str("tenant", m.TenantID).Str("reason", m.Reason).Msg("moved to cold")
	}
	log.Info().Int("moved", moved).Int("planned", len(plan)).Msg("lifecycle cycle complete")
}

// loadTiers reads every tenant's tier config keyed by tenant_id.
func loadTiers(ctx context.Context, pool *pgxpool.Pool) (map[string]lifecycle.TenantTier, error) {
	rows, err := pool.Query(ctx, `SELECT tenant_id::text, hot_days, cold_days FROM tenant_tiers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]lifecycle.TenantTier)
	for rows.Next() {
		var t lifecycle.TenantTier
		if err := rows.Scan(&t.TenantID, &t.HotDays, &t.ColdDays); err != nil {
			return nil, err
		}
		out[t.TenantID] = t
	}
	return out, rows.Err()
}

// snapshotParts reads active partitions on the local (non-cold) volume, with the
// age of their newest data. disk_name 'default' is the hot volume; anything else
// (the s3_cold disk) is already cold and excluded here.
func snapshotParts(ctx context.Context, ch *clickhouse.Client) ([]lifecycle.Partition, error) {
	const q = `
		SELECT table, partition_id, partition, disk_name,
		       toString(dateDiff('day', max(max_time), now())) AS age_days
		FROM system.parts
		WHERE database = 'qeet_logs' AND active = 1
		  AND table IN ('logs','metrics','traces')
		GROUP BY table, partition_id, partition, disk_name
		FORMAT JSON`
	rows, err := ch.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]lifecycle.Partition, 0, len(rows))
	for _, r := range rows {
		vol := lifecycle.VolumeHot
		if str(r["disk_name"]) != "default" {
			vol = lifecycle.VolumeCold
		}
		age, _ := strconv.Atoi(str(r["age_days"]))
		out = append(out, lifecycle.Partition{
			Table:         str(r["table"]),
			Partition:     str(r["partition_id"]),
			TenantID:      tenantFromPartition(str(r["partition"])),
			CurrentVolume: vol,
			AgeDays:       age,
		})
	}
	return out, nil
}

// tenantFromPartition extracts the tenant_id from a ClickHouse partition key
// rendered as "('<tenant-uuid>', <yyyymm>)". Returns "" if it can't be parsed.
func tenantFromPartition(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "(")
	if i := strings.IndexByte(p, ','); i >= 0 {
		p = p[:i]
	}
	return strings.Trim(strings.TrimSpace(p), "'")
}

// escapeID single-quote-escapes a partition id for the ALTER statement.
func escapeID(s string) string { return strings.ReplaceAll(s, "'", "''") }

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
