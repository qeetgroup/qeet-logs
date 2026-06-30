package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// streamDefs are the JetStream streams qeet-logs relies on.
//
// QEET_LOGS is the ingestion pipeline buffer: the Rust gateway publishes parsed,
// PII-masked records to qeet-logs.{tenant_id}.logs; the writer consumes them
// (work-queue) and batch-inserts into ClickHouse. MaxAge bounds the buffer so a
// stalled writer cannot grow the stream unbounded.
var streamDefs = []jetstream.StreamConfig{
	{
		Name:      "QEET_LOGS",
		Subjects:  []string{"qeet-logs.*.logs"},
		Retention: jetstream.WorkQueuePolicy,
		MaxAge:    24 * time.Hour,
	},
}

// EnsureStreams creates or updates all streams idempotently.
func (c *Client) EnsureStreams(ctx context.Context) error {
	for _, def := range streamDefs {
		if _, err := c.JS.CreateOrUpdateStream(ctx, def); err != nil {
			return fmt.Errorf("ensure stream %s: %w", def.Name, err)
		}
	}
	return nil
}

// LogSubject returns the JetStream subject a tenant's parsed logs are published to.
func LogSubject(tenantID string) string {
	return fmt.Sprintf("qeet-logs.%s.logs", tenantID)
}

// TailChannel returns the Redis pub/sub channel used for live tail fan-out.
// The writer publishes each inserted record here so the query API's WebSocket
// tail can stream it without scanning ClickHouse.
func TailChannel(tenantID, service string) string {
	return fmt.Sprintf("tail.%s.%s", tenantID, service)
}
