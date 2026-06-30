package cache

import (
	"github.com/redis/go-redis/v9"
)

// New constructs a Redis client from a redis:// URL. Used for live-tail
// pub/sub, query-result caching, and per-tenant rate-limit counters.
func New(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}
