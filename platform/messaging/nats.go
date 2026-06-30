package messaging

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Client wraps a NATS connection and its JetStream context.
type Client struct {
	Conn *nats.Conn
	JS   jetstream.JetStream
}

// New connects to NATS and creates a JetStream context. name identifies the
// connecting binary (e.g. "qeet-logs-query", "qeet-logs-writer").
func New(url, name string) (*Client, error) {
	nc, err := nats.Connect(url,
		nats.Name(name),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("create JetStream context: %w", err)
	}
	return &Client{Conn: nc, JS: js}, nil
}

// Ping reports whether the NATS connection is currently established.
func (c *Client) Ping() error {
	if c.Conn == nil || !c.Conn.IsConnected() {
		return fmt.Errorf("nats not connected")
	}
	return nil
}

func (c *Client) Close() {
	c.Conn.Drain() //nolint:errcheck
}
