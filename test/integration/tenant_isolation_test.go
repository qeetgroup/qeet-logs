//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
)

// foreignTenant is a UUID no seeded tenant should own; crafted queries attempt
// to pivot to it, and no row must ever come back scoped to it.
const foreignTenant = "00000000-0000-0000-0000-000000000000"

// TestCraftedQueryCannotEscapeTenant is THE headline invariant (TAD §7.2): the
// query layer injects tenant_id from the authenticated identity and NEVER from
// user input. A crafted `q` that names another tenant — directly, via OR, or via
// a parenthesised sub-expression — must not return rows scoped to that tenant.
//
// This is asserted end-to-end: we ask the API to project the real `tenant_id`
// column and verify (a) no returned row carries the foreign tenant, and (b) all
// returned rows share a single tenant_id (no cross-tenant mixing).
func TestCraftedQueryCannotEscapeTenant(t *testing.T) {
	c := apiClient(t)

	crafted := []string{
		// Directly forge the tenant predicate.
		fmt.Sprintf(`SELECT tenant_id, service FROM logs WHERE tenant_id = '%s'`, foreignTenant),
		// Forge it via the `tenant` alias.
		fmt.Sprintf(`SELECT tenant_id, service FROM logs WHERE tenant = '%s'`, foreignTenant),
		// Try to broaden with OR — the outer forced guard must still confine it.
		fmt.Sprintf(`SELECT tenant_id, service FROM logs WHERE tenant = '%s' OR service = 'anything'`, foreignTenant),
		// Parenthesised OR injection.
		fmt.Sprintf(`SELECT tenant_id FROM logs WHERE (tenant_id = '%s' OR service = 'x')`, foreignTenant),
	}

	for _, q := range crafted {
		resp := c.get(qpath("/v1/query", q))
		if resp.Status == http.StatusUnauthorized {
			t.Fatalf("primary key unauthorized: %s", truncate(resp.Body))
		}
		skipIfNoScope(t, resp)
		skipIfUpstreamDown(t, resp)
		requireStatus(t, resp, http.StatusOK)

		var qr queryResult
		resp.decode(t, &qr)

		seen := map[string]struct{}{}
		for _, row := range qr.Rows {
			tid, _ := row["tenant_id"].(string)
			if tid == foreignTenant {
				t.Fatalf("TENANT ISOLATION BREACH: crafted q returned a row for the foreign tenant %s\n  q: %s",
					foreignTenant, q)
			}
			if tid != "" {
				seen[tid] = struct{}{}
			}
		}
		if len(seen) > 1 {
			t.Fatalf("TENANT ISOLATION BREACH: crafted q returned %d distinct tenant_ids %v (expected exactly one)\n  q: %s",
				len(seen), keys(seen), q)
		}
	}
}

// TestCrossTenantResourceIsolation confirms tenant A cannot read/enumerate/delete
// tenant B's admin resources (dashboards, saved searches) — enforced by the
// tenant_id predicate + Postgres RLS. Requires QEET_LOGS_API_KEY_B (a second
// tenant's admin key); skipped otherwise.
func TestCrossTenantResourceIsolation(t *testing.T) {
	a := apiClient(t)

	bKey := tenantBKey()
	if bKey == "" {
		t.Skip("QEET_LOGS_API_KEY_B not set; skipping cross-tenant isolation (needs a 2nd tenant's admin key)")
	}
	b := newClient(t, apiURL(), bKey)

	// Tenant B creates a dashboard.
	dashName := "iso-dash-b-" + uniq()
	created := b.post("/v1/admin/dashboards", map[string]any{"name": dashName, "panels": []any{}})
	if created.Status == http.StatusForbidden || created.Status == http.StatusUnauthorized {
		t.Skipf("tenant B key cannot act as logs:admin (status %d); skipping", created.Status)
	}
	requireStatus(t, created, http.StatusCreated)
	var bDash struct {
		ID string `json:"id"`
	}
	created.decode(t, &bDash)
	defer b.delete("/v1/admin/dashboards/" + bDash.ID)

	requireAdmin(t, a) // tenant A must be admin to attempt the reads below

	// A must NOT be able to fetch B's dashboard by id.
	if got := a.get("/v1/admin/dashboards/" + bDash.ID); got.Status != http.StatusNotFound {
		t.Fatalf("CROSS-TENANT BREACH: tenant A read tenant B's dashboard %s (status %d, want 404)",
			bDash.ID, got.Status)
	}

	// A's dashboard listing must not contain B's dashboard id.
	list := a.get("/v1/admin/dashboards")
	requireStatus(t, list, http.StatusOK)
	if bytes.Contains(list.Body, []byte(bDash.ID)) {
		t.Fatalf("CROSS-TENANT BREACH: tenant A's dashboard list leaks tenant B's dashboard id %s", bDash.ID)
	}

	// A must NOT be able to delete B's dashboard.
	if del := a.delete("/v1/admin/dashboards/" + bDash.ID); del.Status != http.StatusNotFound {
		t.Fatalf("CROSS-TENANT BREACH: tenant A could delete tenant B's dashboard (status %d, want 404)", del.Status)
	}

	// Same shape for saved searches.
	ssName := "iso-search-b-" + uniq()
	bSearch := b.post("/v1/admin/saved-searches", map[string]any{
		"name": ssName, "query_text": "SELECT * FROM logs",
	})
	if bSearch.Status == http.StatusCreated {
		var s struct {
			ID string `json:"id"`
		}
		bSearch.decode(t, &s)
		defer b.delete("/v1/admin/saved-searches/" + s.ID)

		aList := a.get("/v1/admin/saved-searches")
		requireStatus(t, aList, http.StatusOK)
		if bytes.Contains(aList.Body, []byte(s.ID)) {
			t.Fatalf("CROSS-TENANT BREACH: tenant A's saved-search list leaks tenant B's search id %s", s.ID)
		}
		if del := a.delete("/v1/admin/saved-searches/" + s.ID); del.Status != http.StatusNotFound {
			t.Fatalf("CROSS-TENANT BREACH: tenant A deleted tenant B's saved search (status %d, want 404)", del.Status)
		}
	}
}

// TestCrossTenantIncidentsDisjoint confirms the read-only incidents feed of two
// tenants never shares an incident id. Vacuously true when either feed is empty;
// meaningful when both tenants have seeded incidents. Requires QEET_LOGS_API_KEY_B.
func TestCrossTenantIncidentsDisjoint(t *testing.T) {
	a := apiClient(t)
	bKey := tenantBKey()
	if bKey == "" {
		t.Skip("QEET_LOGS_API_KEY_B not set; skipping cross-tenant incidents check")
	}
	b := newClient(t, apiURL(), bKey)

	aResp := a.get("/v1/incidents")
	skipIfNoScope(t, aResp)
	requireStatus(t, aResp, http.StatusOK)
	bResp := b.get("/v1/incidents")
	skipIfNoScope(t, bResp)
	requireStatus(t, bResp, http.StatusOK)

	aIDs := incidentIDs(t, aResp)
	bIDs := incidentIDs(t, bResp)
	for id := range aIDs {
		if _, shared := bIDs[id]; shared {
			t.Fatalf("CROSS-TENANT BREACH: incident id %s appears in BOTH tenants' feeds", id)
		}
	}
}

func incidentIDs(t *testing.T, resp *response) map[string]struct{} {
	t.Helper()
	var body struct {
		Incidents []struct {
			ID string `json:"id"`
		} `json:"incidents"`
	}
	resp.decode(t, &body)
	out := map[string]struct{}{}
	for _, inc := range body.Incidents {
		out[inc.ID] = struct{}{}
	}
	return out
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
