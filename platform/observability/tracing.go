package observability

import (
	"context"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Distributed tracing for the query API (product-readiness). This complements
// the Prometheus RED metrics + structured logs: it emits one OTel server span
// per request, propagating any inbound W3C trace context so a query is
// stitchable into a caller's trace.
//
// It is OPT-IN and zero-overhead until configured: InitTracing is a no-op unless
// OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) points at a
// collector — matching the standard OTel env contract. The Tracing middleware
// uses chi's hijack-safe WrapResponseWriter (same as the metrics layer) so the
// live-tail WebSocket upgrade keeps working, and labels spans by the matched
// route PATTERN (bounded cardinality), never the raw URL.

var tracer = otel.Tracer("qeet-logs-query")

// InitTracing installs an OTLP/HTTP trace exporter + batching tracer provider +
// W3C propagator when an OTLP endpoint is configured. It returns a shutdown
// func (flushes pending spans) and whether tracing was enabled. When no endpoint
// is set it returns a no-op shutdown and enabled=false — never an error — so the
// process runs identically until an operator opts in.
func InitTracing(ctx context.Context, serviceName, version string) (shutdown func(context.Context) error, enabled bool, err error) {
	noop := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return noop, false, nil
	}

	// otlptracehttp reads the standard OTEL_EXPORTER_OTLP_* env (endpoint,
	// headers, TLS) so ops configure it the conventional way.
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, false, err
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.version", version),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	tracer = tp.Tracer("qeet-logs-query")
	return tp.Shutdown, true, nil
}

// Tracing is a chi middleware that starts a server span per request, extracting
// inbound W3C trace context. It records method, matched route, path, and status,
// and marks 5xx responses as errored. Safe to install unconditionally: when
// tracing is not enabled the global provider is a no-op and this adds negligible
// overhead.
func Tracing(next http.Handler) http.Handler {
	prop := otel.GetTextMapPropagator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		span.SetName(r.Method + " " + route)
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", route),
			attribute.String("url.path", r.URL.Path),
			attribute.Int("http.response.status_code", status),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	})
}
