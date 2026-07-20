package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs-server/platform/messaging"
)

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Health is a liveness probe — ok as long as the process is serving.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// Ready is a readiness probe — verifies every backing service is reachable.
// Returns 503 if any dependency is down.
func Ready(pool *pgxpool.Pool, rdb *redis.Client, ch *clickhouse.Client, nc *messaging.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		healthy := true
		record := func(name string, err error) {
			if err != nil {
				checks[name] = "error: " + err.Error()
				healthy = false
				return
			}
			checks[name] = "ok"
		}

		record("postgres", pool.Ping(ctx))
		record("redis", rdb.Ping(ctx).Err())
		record("clickhouse", ch.Ping(ctx))
		record("nats", nc.Ping())

		status, code := "ready", http.StatusOK
		if !healthy {
			status, code = "degraded", http.StatusServiceUnavailable
		}
		writeJSON(w, code, healthResponse{Status: status, Checks: checks})
	}
}

// Version reports the build version stamped via -ldflags.
func Version(v string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": v})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
