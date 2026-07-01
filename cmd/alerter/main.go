// Command alerter is the qeet-logs threshold + absence alert engine (M6).
// It runs as a standalone long-running process alongside cmd/query and the
// Rust ingest services. It polls Postgres for enabled alert rules, evaluates
// each against ClickHouse, and delivers state-change notifications via
// configured channels (webhook, Qeet Notify).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qeetgroup/qeet-logs/domains/alerting"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs/platform/config"
	"github.com/qeetgroup/qeet-logs/platform/database"
	"github.com/qeetgroup/qeet-logs/platform/observability"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	log := observability.New(cfg.Env)
	log.Info().Str("version", version).Msg("qeet-logs alerter starting")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer pool.Close()

	ch := clickhouse.New(cfg.ClickHouseURL, cfg.ClickHouseDatabase, cfg.ClickHouseUser, cfg.ClickHousePassword)

	// Verify ClickHouse is reachable before entering the eval loop.
	if err := ch.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("clickhouse ping")
	}

	interval := 60 * time.Second
	engine := alerting.New(pool, ch, cfg.QeetNotifyURL, cfg.QeetNotifyAPIKey, interval, log)
	engine.Run(ctx)
}
