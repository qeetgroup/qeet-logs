package config

import "github.com/kelseyhightower/envconfig"

// Config is the runtime configuration for all qeet-logs Go binaries, loaded
// from the environment (see .env.example). Defaults target local dev.
type Config struct {
	// Server
	HTTPPort string `envconfig:"HTTP_PORT" default:"8100"`
	Env      string `envconfig:"ENV" default:"development"`

	// PostgreSQL (metadata: tenants, api keys, alert rules, audit log)
	DatabaseURL   string `envconfig:"DATABASE_URL" required:"true"`
	MigrationsDir string `envconfig:"MIGRATIONS_DIR" default:"migrations"`

	// ClickHouse (log storage)
	ClickHouseURL      string `envconfig:"CLICKHOUSE_URL" default:"http://localhost:8123"`
	ClickHouseDatabase string `envconfig:"CLICKHOUSE_DATABASE" default:"qeet_logs"`
	ClickHouseUser     string `envconfig:"CLICKHOUSE_USER" default:"default"`
	ClickHousePassword string `envconfig:"CLICKHOUSE_PASSWORD" default:""`

	// NATS JetStream (ingestion bus)
	NATSURL string `envconfig:"NATS_URL" default:"nats://localhost:4223"`

	// Redis (live-tail pub/sub, query cache, rate limits)
	RedisURL string `envconfig:"REDIS_URL" default:"redis://localhost:6380"`

	// Auth (Qeet ID OIDC)
	QeetIDIssuer string `envconfig:"QEET_ID_ISSUER" default:"https://api.id.qeet.in"`
	CookieDomain string `envconfig:"COOKIE_DOMAIN" default:".logs.qeet.in"`

	// Cold/archive tier object storage (S3 / MinIO) — Phase 2
	S3Bucket   string `envconfig:"S3_BUCKET" default:""`
	S3Endpoint string `envconfig:"S3_ENDPOINT" default:""`
	S3Region   string `envconfig:"S3_REGION" default:"ap-south-1"`

	// Alert delivery via Qeet Notify — M6
	QeetNotifyURL    string `envconfig:"QEET_NOTIFY_URL" default:"https://api.notify.qeet.in"`
	QeetNotifyAPIKey string `envconfig:"QEET_NOTIFY_API_KEY" default:""`
}

func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, err
	}
	return &c, nil
}
