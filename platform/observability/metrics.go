package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTP self-metrics for the query API (product-readiness / PRD Module 02 self-
// observability). Exposes RED-style metrics (Rate, Errors, Duration) on a
// Prometheus endpoint so operators can scrape request throughput, latency, and
// error ratios per route. This complements — it does not replace — the
// structured zerolog logs and the RequestID/RealIP middleware already in place.
//
// The route LABEL is the matched chi route PATTERN (e.g. "/v1/query"), never the
// raw URL, so high-cardinality path segments (ids, tenant uuids) can never blow
// up the metrics series count. Unmatched requests collapse to "unmatched".

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "qeet_logs",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests handled, by method, matched route pattern, and status class.",
	}, []string{"method", "route", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "qeet_logs",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency in seconds, by method and matched route pattern.",
		// Buckets tuned for a query API: sub-ms health checks up to multi-second
		// ClickHouse scans.
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "route"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "qeet_logs",
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "In-flight HTTP requests currently being served.",
	})
)

// MetricsHandler returns the Prometheus scrape handler (default registry, which
// also carries the Go runtime + process collectors). Mount it at /metrics.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// Metrics is a chi middleware that records request count, latency, and in-flight
// gauge for every request. It uses chi's WrapResponseWriter so the wrapped
// writer still satisfies http.Hijacker/Flusher — the live-tail WebSocket upgrade
// and streaming responses keep working. The route pattern is read AFTER the
// handler runs (chi fills it during routing).
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		elapsed := time.Since(start).Seconds()

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // handler wrote body without an explicit WriteHeader
		}

		httpRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route).Observe(elapsed)
	})
}
