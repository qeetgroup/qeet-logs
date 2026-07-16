// Command query is the qeet-logs query/API server: it serves the REST query
// API, the live-tail WebSocket, and admin endpoints. M0 stands up the process
// with health/readiness probes; query routes are added in M3+.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/qeetgroup/qeet-logs/platform/api/handler"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/cache"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs/platform/config"
	"github.com/qeetgroup/qeet-logs/platform/database"
	"github.com/qeetgroup/qeet-logs/platform/messaging"
	"github.com/qeetgroup/qeet-logs/platform/observability"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	log := observability.New(cfg.Env)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to postgres")
	}
	defer pool.Close()

	rdb, err := cache.New(cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to redis")
	}
	defer rdb.Close()

	ch := clickhouse.New(cfg.ClickHouseURL, cfg.ClickHouseDatabase, cfg.ClickHouseUser, cfg.ClickHousePassword)

	nc, err := messaging.New(cfg.NATSURL, "qeet-logs-query")
	if err != nil {
		log.Fatal().Err(err).Msg("connect to NATS")
	}
	defer nc.Close()

	if err := nc.EnsureStreams(ctx); err != nil {
		log.Fatal().Err(err).Msg("ensure NATS streams")
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Qeet-Api-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	// Security + observability baseline (product-readiness).
	r.Use(apimw.SecureHeaders)                           // OWASP secure-header set
	r.Use(apimw.MaxBodyBytes(apimw.DefaultMaxBodyBytes)) // 4 MiB request-body cap
	r.Use(observability.Metrics)                         // Prometheus RED metrics

	r.Get("/healthz", handler.Health)
	r.Get("/readyz", handler.Ready(pool, rdb, ch, nc))
	r.Get("/version", handler.Version(version))
	// Prometheus scrape endpoint (RED metrics + Go runtime/process collectors).
	r.Handle("/metrics", observability.MetricsHandler())

	// Per-tenant rate-limit config (RATE_LIMIT_PER_MINUTE overrides the default).
	rlCfg := apimw.DefaultRateLimit
	if v := os.Getenv("RATE_LIMIT_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rlCfg = apimw.RateLimitConfig{Requests: n, Window: time.Minute}
		}
	}

	// Authenticated query API (API key → tenant + scopes).
	r.Route("/v1", func(rt chi.Router) {
		rt.Use(apimw.APIKeyAuth(pool))
		rt.Use(apimw.RateLimit(rdb, rlCfg))
		rt.Get("/query", handler.Query(ch, pool))
		rt.Get("/query/tail", handler.Tail(rdb))
		rt.Get("/auth-events", handler.AuthEvents(ch, pool))

		// Change-event ingestion + listing (Module 15.1)
		rt.Post("/changes", handler.CreateChange(ch, pool))
		rt.Get("/changes", handler.ListChanges(ch, pool))
		// Provider webhook connectors → change_events (Module 30.4 / 31.3 / 31.4)
		rt.Post("/changes/{provider}", handler.ChangeConnector(ch, pool))

		// Service dependency & topology graph (Module 10)
		rt.Get("/topology", handler.Topology(ch, pool))

		// Unified Investigation Timeline (Module 09)
		rt.Get("/timeline", handler.Timeline(ch, pool))

		// Correlated incidents + low-severity feed (Module 13)
		rt.Get("/incidents", handler.ListIncidents(pool))

		// RCA structural retrieval (Module 11.1)
		rt.Get("/rca", handler.RCA(ch, pool))

		// Deployment Intelligence — ranked culprit scoring + health + rollback (Module 15.2–15.4)
		rt.Get("/deploy/culprits", handler.DeployCulprits(ch, pool))

		// Business Context Correlation Layer — exposure tags (Module 16 / P2-G4)
		rt.Get("/business-context", handler.BusContextByService(pool))
		rt.Get("/incidents/{id}/context", handler.BusContextForIncident(pool))

		// Export/programmatic access, routing simulation, TTFIQ (Modules 8.5/17.4/7.5 / P2-G16)
		rt.Get("/export", handler.Export(ch, pool))
		rt.Post("/alerts/simulate", handler.SimulateAlertRouting(pool))
		rt.Get("/analytics/ttfiq", handler.TTFIQ(pool))

		// Predictive Observability — statistical capacity/exhaustion + trend (Module 14.1/14.2 / P2-G12)
		rt.Get("/forecast", handler.Forecast(ch))
		// AI Copilot GA — governed LLM (opt-in + PII-mask + audit) (Module 12 / P2-G11)
		rt.Post("/query/copilot", handler.Copilot(pool))
		// AI Copilot conversational multi-turn — durable threads over the same
		// governed pipeline (Module 12.2 / P2-G11)
		rt.Post("/query/copilot/conversations", handler.StartCopilotConversation(pool))
		rt.Post("/query/copilot/conversations/{id}/messages", handler.CopilotConversationMessage(pool))
		rt.Get("/query/copilot/conversations/{id}", handler.GetCopilotConversation(pool))

		// Correlation-aware panel overlays (Module 22.2)
		rt.Get("/overlays", handler.Overlays(ch, pool))

		// NL-to-query translation (G18 / Module 4.8)
		rt.Post("/query/nl", handler.NLQuery())
	})

	// Public, unauthenticated shared-dashboard read (Module 22.3) — seat-free.
	r.Get("/shared/dashboards/{token}", handler.GetSharedDashboard(pool))

	// Prometheus-compatible query API (PRD Module 02.2) — point a Grafana
	// Prometheus data source here; auth via X-Qeet-Api-Key → tenant + scopes.
	r.Route("/api/v1", func(rt chi.Router) {
		rt.Use(apimw.APIKeyAuth(pool))
		rt.Use(apimw.RateLimit(rdb, rlCfg))
		rt.Get("/query", handler.PromInstantQuery(ch, pool))
		rt.Post("/query", handler.PromInstantQuery(ch, pool))
		rt.Get("/query_range", handler.PromRangeQuery(ch, pool))
		rt.Post("/query_range", handler.PromRangeQuery(ch, pool))
	})

	// Grafana Loki-compatible read data source (Module 22.4 / P2-G13) — point a
	// Grafana "Loki" data source here; auth via X-Qeet-Api-Key → tenant + scopes.
	r.Route("/loki/api/v1", func(rt chi.Router) {
		rt.Use(apimw.APIKeyAuth(pool))
		rt.Use(apimw.RateLimit(rdb, rlCfg))
		rt.Get("/query_range", handler.GrafanaLokiQueryRange(ch, pool))
		rt.Get("/labels", handler.GrafanaLokiLabels(ch, pool))
		rt.Get("/label/{name}/values", handler.GrafanaLokiLabelValues(ch, pool))
		rt.Get("/series", handler.GrafanaLokiSeries(ch, pool))
		rt.Get("/status/buildinfo", handler.GrafanaLokiBuildInfo())
	})

	// Two-way ChatOps — Slack OAuth install + slash-commands (Module 19.1/19.3 /
	// P2-G7). Slack calls these directly (they carry a workspace team_id, not a
	// Qeet API key), so this group is NOT behind APIKeyAuth — each handler
	// verifies Slack's request signature and resolves the tenant from the
	// installation record, and returns 501 until the Slack app secrets are set.
	r.Route("/v1/chatops", func(rt chi.Router) {
		rt.Get("/slack/install", handler.ChatOpsSlackInstall())
		rt.Get("/slack/callback", handler.ChatOpsSlackCallback(pool))
		rt.Post("/slack/commands", handler.ChatOpsSlackCommands(ch, pool))
		rt.Post("/slack/interactivity", handler.ChatOpsSlackInteractivity())
	})

	// Admin API — accepts an API key with logs:admin OR a Qeet ID Bearer JWT.
	r.Route("/v1/admin", func(rt chi.Router) {
		rt.Use(apimw.AnyAuth(pool, cfg.QeetIDIssuer))
		rt.Use(apimw.RequireScope("logs:admin"))

		// API key CRUD
		rt.Post("/api-keys", handler.CreateAPIKey(pool))
		rt.Get("/api-keys", handler.ListAPIKeys(pool))
		rt.Delete("/api-keys/{id}", handler.RevokeAPIKey(pool))

		// Alert rules CRUD (M6)
		rt.Get("/alert-rules", handler.ListAlertRules(pool))
		rt.Post("/alert-rules", handler.CreateAlertRule(pool))
		rt.Delete("/alert-rules/{id}", handler.DeleteAlertRule(pool))

		// Retention config (M6)
		rt.Get("/retention", handler.GetRetention(pool))
		rt.Put("/retention", handler.UpdateRetention(pool))
		// Cost-transparent retention estimate + what-if (Module 6.4 / P2-G2)
		rt.Get("/retention/cost", handler.RetentionCost(ch, pool))

		// AI feature opt-in (Module 12 / P2-G11)
		rt.Get("/ai-features", handler.GetAIFeatures(pool))
		rt.Put("/ai-features", handler.UpdateAIFeatures(pool))
		// One-way ChatOps delivery test (Module 19 outbound / P2-G7)
		rt.Post("/chatops/test", handler.ChatOpsTest())
		// Regional-language alert-delivery preference via Qeet Notify (Module 27.5 / P2-G8)
		rt.Get("/notify-locale", handler.GetNotifyLocale(pool))
		rt.Put("/notify-locale", handler.SetNotifyLocale(pool))
		// Plans / quota / overage + invoice preview (Module 33.4 / P2-G17)
		rt.Get("/plan", handler.BillingGetPlan(pool))
		rt.Put("/plan", handler.BillingSetPlan(pool))
		rt.Get("/billing/preview", handler.BillingPreview(ch, pool))
		// RCA root-cause labels → training corpus for the deferred learned ranker (Module 11.2 / P2-G10)
		rt.Post("/rca/feedback", handler.RCAFeedback(pool))

		// Outbound webhook endpoints (Module 30.4)
		rt.Post("/webhooks", handler.CreateWebhook(pool))
		rt.Get("/webhooks", handler.ListWebhooks(pool))
		rt.Delete("/webhooks/{id}", handler.DeleteWebhook(pool))

		// Incident feedback → continuous calibration (Module 13.3)
		rt.Post("/incidents/{id}/feedback", handler.SubmitIncidentFeedback(pool))

		// Incident war-room core — declare / timeline / roles / handoff (Module 18 / P2-G5)
		rt.Post("/incidents/{id}/declare", handler.DeclareIncidentWarRoom(pool))
		rt.Get("/incidents/{id}/session", handler.GetIncidentWarRoom(pool))
		rt.Post("/sessions/{id}/entries", handler.AddWarRoomEntry(pool))
		rt.Post("/sessions/{id}/roles", handler.AssignWarRoomRole(pool))
		rt.Post("/sessions/{id}/handoff", handler.HandoffWarRoom(pool))

		// Business context mappings CRUD (Module 16 / P2-G4)
		rt.Post("/business-context", handler.BusContextCreate(pool))
		rt.Get("/business-context", handler.BusContextList(pool))
		rt.Delete("/business-context/{id}", handler.BusContextDelete(pool))

		// Postmortems + remediation commitments (Module 20) + CERT-In export (Module 27.2 / P2-G6)
		rt.Post("/postmortems", handler.CreatePostmortem(pool))
		rt.Get("/postmortems", handler.ListPostmortems(pool))
		rt.Get("/postmortems/{id}", handler.GetPostmortem(pool))
		rt.Patch("/postmortems/{id}", handler.UpdatePostmortem(pool))
		rt.Post("/postmortems/{id}/commitments", handler.CreatePostmortemCommitment(pool))
		rt.Get("/postmortems/{id}/commitments", handler.ListPostmortemCommitments(pool))
		rt.Get("/postmortems/{id}/cert-in-export", handler.ExportPostmortemCERTIn(pool))

		// In-flight remap program (Module 04.2)
		rt.Get("/transform", handler.GetTransform(pool))
		rt.Put("/transform", handler.UpsertTransform(pool))

		// Audit log (M6)
		rt.Get("/audit", handler.ListAudit(pool))

		// Dashboards CRUD (M8)
		rt.Get("/dashboards", handler.ListDashboards(pool))
		rt.Post("/dashboards", handler.CreateDashboard(pool))
		rt.Get("/dashboards/{id}", handler.GetDashboard(pool))
		rt.Put("/dashboards/{id}", handler.UpdateDashboard(pool))
		rt.Delete("/dashboards/{id}", handler.DeleteDashboard(pool))
		rt.Post("/dashboards/{id}/share", handler.ShareDashboard(pool))

		// Saved searches CRUD (M8)
		rt.Get("/saved-searches", handler.ListSavedSearches(pool))
		rt.Post("/saved-searches", handler.CreateSavedSearch(pool))
		rt.Delete("/saved-searches/{id}", handler.DeleteSavedSearch(pool))

		// DLQ replay API (M9)
		rt.Get("/dlq", handler.ListDLQ(pool))
		rt.Post("/dlq/{id}/replay", handler.ReplayDLQ(pool, nc.Conn))
		rt.Delete("/dlq/{id}", handler.DropDLQ(pool))

		// Quota usage (M9)
		rt.Get("/quota/usage", handler.QuotaUsage(ch, pool))

		// DPDP / GDPR erasure (G17)
		rt.Post("/erasure", handler.CreateErasure(pool, ch))
		rt.Get("/erasure", handler.ListErasure(pool))
	})

	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info().Str("port", cfg.HTTPPort).Str("version", version).Msg("qeet-logs query api starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("query api server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx) //nolint:errcheck
}
