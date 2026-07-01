package qeetlogs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// LogRecord is a single structured log entry to ingest.
type LogRecord struct {
	// Timestamp defaults to time.Now() when zero.
	Timestamp time.Time `json:"timestamp,omitempty"`
	// Service is the originating service name. Required.
	Service string `json:"service"`
	// Level is the severity level (debug/info/warn/error/fatal).
	Level string `json:"level,omitempty"`
	// TraceID links this record to a distributed trace.
	TraceID string `json:"trace_id,omitempty"`
	// Body is the raw log message. Required.
	Body string `json:"body"`
	// Attributes holds arbitrary structured key-value metadata.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// IngestBatch sends a batch of log records to the ingest endpoint.
// Records are written to ClickHouse via the NATS JetStream bus.
func (c *Client) IngestBatch(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	// Stamp records that have no timestamp.
	now := time.Now().UTC()
	stamped := make([]LogRecord, len(records))
	for i, r := range records {
		if r.Timestamp.IsZero() {
			r.Timestamp = now
		}
		stamped[i] = r
	}
	return c.do(ctx, "POST", "/v1/ingest", map[string]any{"records": stamped}, nil)
}

// Ingest is a convenience wrapper that sends a single record.
func (c *Client) Ingest(ctx context.Context, r LogRecord) error {
	return c.IngestBatch(ctx, []LogRecord{r})
}

// IngestBuilder lets callers chain field setters before calling Send().
type IngestBuilder struct {
	client *Client
	record LogRecord
}

// Log returns an IngestBuilder for a record in the given service.
func (c *Client) Log(service string) *IngestBuilder {
	return &IngestBuilder{
		client: c,
		record: LogRecord{Service: service},
	}
}

func (b *IngestBuilder) Level(level string) *IngestBuilder   { b.record.Level = level; return b }
func (b *IngestBuilder) Body(body string) *IngestBuilder     { b.record.Body = body; return b }
func (b *IngestBuilder) Trace(traceID string) *IngestBuilder { b.record.TraceID = traceID; return b }
func (b *IngestBuilder) Attr(key string, val any) *IngestBuilder {
	if b.record.Attributes == nil {
		b.record.Attributes = make(map[string]any)
	}
	b.record.Attributes[key] = val
	return b
}

// NewTrace generates and attaches a fresh UUID-based trace ID.
func (b *IngestBuilder) NewTrace() *IngestBuilder {
	b.record.TraceID = uuid.NewString()
	return b
}

// Send emits the record.
func (b *IngestBuilder) Send(ctx context.Context) error {
	if b.record.Body == "" {
		return fmt.Errorf("qeetlogs: Body is required")
	}
	return b.client.Ingest(ctx, b.record)
}
