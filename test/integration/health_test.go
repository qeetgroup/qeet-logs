//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// TestHealthz asserts the liveness probe is unauthenticated and reports "ok".
func TestHealthz(t *testing.T) {
	c := publicClient(t)
	resp := c.get("/healthz")
	requireStatus(t, resp, http.StatusOK)

	var body struct {
		Status string `json:"status"`
	}
	resp.decode(t, &body)
	if body.Status != "ok" {
		t.Fatalf("healthz status = %q, want %q", body.Status, "ok")
	}
}

// TestReadyz asserts the readiness probe reports on every backing dependency.
// 200 => ready; 503 => a dependency is down (still a valid, well-formed reply).
func TestReadyz(t *testing.T) {
	c := publicClient(t)
	resp := c.get("/readyz")
	requireStatus(t, resp, http.StatusOK, http.StatusServiceUnavailable)

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	resp.decode(t, &body)

	if resp.Status == http.StatusOK && body.Status != "ready" {
		t.Fatalf("readyz 200 but status = %q, want %q", body.Status, "ready")
	}
	if resp.Status == http.StatusServiceUnavailable {
		t.Logf("readyz degraded — dependency checks: %v", body.Checks)
	}
	// The probe checks postgres, redis, clickhouse and nats.
	for _, dep := range []string{"postgres", "redis", "clickhouse", "nats"} {
		if _, ok := body.Checks[dep]; !ok {
			t.Errorf("readyz missing dependency check %q (checks: %v)", dep, body.Checks)
		}
	}
}

// TestVersion asserts the build-version endpoint is unauthenticated and stamped.
func TestVersion(t *testing.T) {
	c := publicClient(t)
	resp := c.get("/version")
	requireStatus(t, resp, http.StatusOK)

	var body struct {
		Version string `json:"version"`
	}
	resp.decode(t, &body)
	if body.Version == "" {
		t.Fatalf("version endpoint returned an empty version")
	}
}
