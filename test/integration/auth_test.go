//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// TestMissingAPIKeyUnauthorized asserts an authenticated route rejects a request
// with no X-Qeet-Api-Key header (401).
func TestMissingAPIKeyUnauthorized(t *testing.T) {
	publicClient(t)                    // reachable-check only (skip if API is down)
	anon := newClient(t, apiURL(), "") // deliberately keyless
	resp := anon.get(qpath("/v1/query", "SELECT * FROM logs"))
	requireStatus(t, resp, http.StatusUnauthorized)
	if msg := resp.errorMessage(); msg == "" {
		t.Errorf("401 body should carry an {\"error\": …}; got: %s", truncate(resp.Body))
	}
}

// TestInvalidAPIKeyUnauthorized asserts a syntactically-fine but unknown key is
// rejected (401), never silently resolved to a tenant.
func TestInvalidAPIKeyUnauthorized(t *testing.T) {
	publicClient(t) // skip if API unreachable
	bogus := newClient(t, apiURL(), "qeel_this-key-does-not-exist-000000000000")
	resp := bogus.get(qpath("/v1/query", "SELECT * FROM logs"))
	requireStatus(t, resp, http.StatusUnauthorized)
}

// TestValidKeyResolves asserts the primary key authenticates successfully — the
// request is NOT rejected at the auth layer (401) nor the scope layer (403).
func TestValidKeyResolves(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "SELECT * FROM logs"))
	if resp.Status == http.StatusUnauthorized {
		t.Fatalf("valid key was rejected as unauthorized: %s", truncate(resp.Body))
	}
	skipIfNoScope(t, resp) // key may lack logs:read/query — that is not an auth failure
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)
}

// TestAdminScopeEnforced is the RBAC invariant: a key lacking logs:admin gets
// 403 on an admin route (never 200/401). It uses QEET_LOGS_API_KEY_READONLY if
// set, otherwise mints a logs:read-only key with the admin key and cleans it up.
func TestAdminScopeEnforced(t *testing.T) {
	admin := apiClient(t)

	roKey := readOnlyKey()
	if roKey == "" {
		var id string
		roKey, id = mintScopedKey(t, admin, []string{"logs:read"})
		defer revokeKey(admin, id)
	}

	ro := newClient(t, apiURL(), roKey)

	// The read-only key must authenticate (200) on a read route it is allowed on…
	if resp := ro.get(qpath("/v1/query", "SELECT * FROM logs")); resp.Status == http.StatusUnauthorized {
		t.Fatalf("read-only key failed to authenticate: %s", truncate(resp.Body))
	}

	// …but must be denied (403) on an admin route it lacks logs:admin for.
	resp := ro.get("/v1/admin/api-keys")
	if resp.Status != http.StatusForbidden {
		t.Fatalf("non-admin key on admin route: status %d, want 403 (RBAC breach if 200)\n  body: %s",
			resp.Status, truncate(resp.Body))
	}
}
