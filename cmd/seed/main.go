// Command seed provisions a local dev dataset: two demo tenants (so cross-tenant
// isolation can be exercised), an API key per tenant with the full logs:* scope
// set, and a handful of sample log rows in ClickHouse. It is idempotent on the
// tenant slug; each run mints fresh API keys (shown once) and appends sample
// logs. Referenced by the Makefile `seed` target.
//
// It degrades gracefully: Postgres seeding (tenants + keys) always runs; if
// ClickHouse is unreachable the sample-log step is skipped with a warning, so
// `make seed` still yields working credentials for the auth/admin/query surface.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs-server/platform/config"
	"github.com/qeetgroup/qeet-logs-server/platform/database"
	"github.com/qeetgroup/qeet-logs-server/platform/observability"
)

// allScopes is every logs:* scope except the cross-tenant operator scope.
var allScopes = []string{"logs:ingest", "logs:read", "logs:query", "logs:export", "logs:admin"}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	log := observability.New(cfg.Env)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer pool.Close()

	// Two tenants: primary + a second one so cross-tenant isolation is testable.
	tenantA, err := upsertTenant(ctx, pool, "demo", "Demo Tenant")
	if err != nil {
		log.Fatal().Err(err).Msg("seed tenant demo")
	}
	tenantB, err := upsertTenant(ctx, pool, "demo-b", "Demo Tenant B")
	if err != nil {
		log.Fatal().Err(err).Msg("seed tenant demo-b")
	}

	keyA, err := database.CreateAPIKey(ctx, pool, tenantA, "seed-key", allScopes, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("create api key A")
	}
	keyB, err := database.CreateAPIKey(ctx, pool, tenantB, "seed-key-b", allScopes, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("create api key B")
	}

	// Sample logs — best-effort; skip cleanly if ClickHouse is not up.
	ch := clickhouse.New(cfg.ClickHouseURL, cfg.ClickHouseDatabase, cfg.ClickHouseUser, cfg.ClickHousePassword)
	if err := ch.Ping(ctx); err != nil {
		log.Warn().Err(err).Msg("clickhouse unreachable — skipping sample logs (tenants + keys still seeded)")
	} else if n, err := seedLogs(ctx, ch, tenantA); err != nil {
		log.Warn().Err(err).Msg("seed sample logs failed")
	} else {
		log.Info().Int("rows", n).Msg("seeded sample logs")
	}

	fmt.Println("\n─────────────────────────────────────────────────────────────")
	fmt.Println(" qeet-logs seed complete")
	fmt.Println("─────────────────────────────────────────────────────────────")
	fmt.Printf(" Tenant A  %s  (slug=demo)\n   API key: %s\n", tenantA, keyA.Key)
	fmt.Printf(" Tenant B  %s  (slug=demo-b)\n   API key: %s\n", tenantB, keyB.Key)
	fmt.Println("─────────────────────────────────────────────────────────────")
	fmt.Println(" Run the integration suite against these:")
	fmt.Printf("   export QEET_LOGS_API_KEY=%s\n", keyA.Key)
	fmt.Printf("   export QEET_LOGS_API_KEY_B=%s\n", keyB.Key)
	fmt.Println("   go test -tags=integration ./test/integration/...")
	fmt.Println("─────────────────────────────────────────────────────────────")
}

// upsertTenant creates (or returns the existing) tenant for slug and returns its
// id. Idempotent on slug so repeated seeding is safe.
func upsertTenant(ctx context.Context, pool *pgxpool.Pool, slug, name string) (string, error) {
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug) VALUES ($1, $2)
		 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		 RETURNING id::text`, name, slug).Scan(&id)
	return id, err
}

// seedLogs inserts a spread of sample log rows for the tenant across the last
// hour, exercising multiple services and levels.
func seedLogs(ctx context.Context, ch *clickhouse.Client, tenantID string) (int, error) {
	services := []string{"checkout-api", "payments", "auth", "search"}
	levels := []string{"info", "info", "info", "warn", "error"}
	messages := map[string]string{
		"info":  "request handled ok",
		"warn":  "elevated latency on downstream dependency",
		"error": "unhandled exception while processing request",
	}

	now := time.Now().UTC()
	rows := make([]map[string]any, 0, 40)
	for i := 0; i < 40; i++ {
		svc := services[i%len(services)]
		lvl := levels[i%len(levels)]
		ts := now.Add(-time.Duration(i) * 90 * time.Second)
		rows = append(rows, map[string]any{
			"id":              newID(),
			"timestamp":       ts.Format("2006-01-02 15:04:05.000000000"),
			"tenant_id":       tenantID,
			"service":         svc,
			"environment":     "production",
			"level":           lvl,
			"message":         fmt.Sprintf("[%s] %s", svc, messages[lvl]),
			"_retention_days": 7,
		})
	}
	if err := ch.Insert(ctx, "qeet_logs.logs", rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// newID returns a random 26-char lowercase hex id (ULID-shaped enough for a seed;
// production ids come from the Rust ingest core's ULID generator).
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:26]
}
