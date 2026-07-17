package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitTracingNoopWithoutEndpoint(t *testing.T) {
	// No OTEL endpoint configured → disabled, no error, usable shutdown.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	shutdown, enabled, err := InitTracing(context.Background(), "qeet-logs-query", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("tracing must be disabled when no OTLP endpoint is set")
	}
	if shutdown == nil {
		t.Fatal("shutdown must never be nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown should not error: %v", err)
	}
}

func TestTracingMiddlewarePassesThrough(t *testing.T) {
	// With tracing disabled (global no-op provider), the middleware must be a
	// transparent pass-through that preserves the handler's status.
	h := Tracing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/query", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rec.Body.String())
	}
}
