//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"testing"
)

// Admin CRUD roundtrips. Every test requires the primary key to act as
// logs:admin (RequireScope("logs:admin") on the /v1/admin group); requireAdmin
// skips the test otherwise. Each test creates its own resource and cleans it up,
// so the suite is idempotent and leaves no residue.

// TestAdminAPIKeysCRUD: create → list (present) → revoke → list (absent).
func TestAdminAPIKeysCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/api-keys", map[string]any{
		"name":   "integration-crud-" + uniq(),
		"scopes": []string{"logs:read"},
	})
	requireStatus(t, created, http.StatusCreated)
	var nk newAPIKey
	created.decode(t, &nk)
	if nk.ID == "" || nk.Key == "" {
		t.Fatalf("created key missing ID/Key: %s", truncate(created.Body))
	}

	list := c.get("/v1/admin/api-keys")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, nk.ID) {
		t.Errorf("newly created key %s missing from list", nk.ID)
	}

	requireStatus(t, c.delete("/v1/admin/api-keys/"+nk.ID), http.StatusNoContent)

	after := c.get("/v1/admin/api-keys")
	requireStatus(t, after, http.StatusOK)
	if containsID(after.Body, nk.ID) {
		t.Errorf("revoked key %s still present in list", nk.ID)
	}
}

// TestAdminAlertRulesCRUD: create threshold rule → list (present) → delete.
func TestAdminAlertRulesCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/alert-rules", map[string]any{
		"name":           "integration-rule-" + uniq(),
		"kind":           "threshold",
		"threshold":      5,
		"window_seconds": 300,
		"channels":       []any{},
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	list := c.get("/v1/admin/alert-rules")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, id) {
		t.Errorf("created alert rule %s missing from list", id)
	}
	requireStatus(t, c.delete("/v1/admin/alert-rules/"+id), http.StatusNoContent)
}

// TestAdminDashboardsCRUD: create → get → update → list → delete.
func TestAdminDashboardsCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/dashboards", map[string]any{
		"name":   "integration-dash-" + uniq(),
		"panels": []any{map[string]any{"type": "timeseries"}},
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	requireStatus(t, c.get("/v1/admin/dashboards/"+id), http.StatusOK)

	updated := c.put("/v1/admin/dashboards/"+id, map[string]any{
		"name":   "integration-dash-updated-" + uniq(),
		"panels": []any{},
	})
	requireStatus(t, updated, http.StatusOK)

	list := c.get("/v1/admin/dashboards")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, id) {
		t.Errorf("created dashboard %s missing from list", id)
	}
	requireStatus(t, c.delete("/v1/admin/dashboards/"+id), http.StatusNoContent)
}

// TestAdminSavedSearchesCRUD: create → list → delete.
func TestAdminSavedSearchesCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/saved-searches", map[string]any{
		"name":       "integration-search-" + uniq(),
		"query_text": "SELECT * FROM logs WHERE level = 'error'",
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	list := c.get("/v1/admin/saved-searches")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, id) {
		t.Errorf("created saved search %s missing from list", id)
	}
	requireStatus(t, c.delete("/v1/admin/saved-searches/"+id), http.StatusNoContent)
}

// TestAdminRetention: get → update → verify → restore.
func TestAdminRetention(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	before := c.get("/v1/admin/retention")
	requireStatus(t, before, http.StatusOK)
	var cur struct {
		RetentionDays int `json:"retention_days"`
	}
	before.decode(t, &cur)
	if cur.RetentionDays < 1 {
		t.Fatalf("retention_days = %d, want >= 1", cur.RetentionDays)
	}

	newDays := cur.RetentionDays + 7
	if newDays > 3650 {
		newDays = cur.RetentionDays - 1
	}
	upd := c.put("/v1/admin/retention", map[string]any{
		"retention_days":  newDays,
		"masking_actions": map[string]string{},
	})
	requireStatus(t, upd, http.StatusOK)
	var updBody struct {
		RetentionDays int `json:"retention_days"`
	}
	upd.decode(t, &updBody)
	if updBody.RetentionDays != newDays {
		t.Errorf("after update retention_days = %d, want %d", updBody.RetentionDays, newDays)
	}

	// Restore the original value.
	requireStatus(t, c.put("/v1/admin/retention", map[string]any{
		"retention_days":  cur.RetentionDays,
		"masking_actions": map[string]string{},
	}), http.StatusOK)
}

// TestAdminBusinessContextCRUD: create mapping → list → delete.
func TestAdminBusinessContextCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/business-context", map[string]any{
		"service":         "integration-svc-" + uniq(),
		"customer":        "Acme",
		"plan_tier":       "enterprise",
		"monthly_revenue": 12000.0,
		"sla_target":      99.9,
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	list := c.get("/v1/admin/business-context")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, id) {
		t.Errorf("created business-context mapping %s missing from list", id)
	}
	requireStatus(t, c.delete("/v1/admin/business-context/"+id), http.StatusNoContent)
}

// TestAdminPostmortemsCRUD: create → get → patch(publish) → commitment create/list.
// (Postmortems are permanent records — there is no delete route by design.)
func TestAdminPostmortemsCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/postmortems", map[string]any{
		"title":   "integration-postmortem-" + uniq(),
		"summary": "created by the integration suite",
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	requireStatus(t, c.get("/v1/admin/postmortems/"+id), http.StatusOK)

	patched := c.patch("/v1/admin/postmortems/"+id, map[string]any{"status": "published"})
	requireStatus(t, patched, http.StatusOK)
	var pm struct {
		Status string `json:"status"`
	}
	patched.decode(t, &pm)
	if pm.Status != "published" {
		t.Errorf("postmortem status after publish = %q, want %q", pm.Status, "published")
	}

	commit := c.post("/v1/admin/postmortems/"+id+"/commitments", map[string]any{
		"description": "add alert coverage",
	})
	requireStatus(t, commit, http.StatusCreated)

	requireStatus(t, c.get("/v1/admin/postmortems/"+id+"/commitments"), http.StatusOK)
}

// TestAdminWebhooksCRUD: create → list → delete.
func TestAdminWebhooksCRUD(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	created := c.post("/v1/admin/webhooks", map[string]any{
		"url":         "https://example.test/hook/" + uniq(),
		"events":      []string{"incident.created"},
		"description": "integration",
	})
	requireStatus(t, created, http.StatusCreated)
	id := idField(t, created)

	list := c.get("/v1/admin/webhooks")
	requireStatus(t, list, http.StatusOK)
	if !containsID(list.Body, id) {
		t.Errorf("created webhook %s missing from list", id)
	}
	requireStatus(t, c.delete("/v1/admin/webhooks/"+id), http.StatusNoContent)
}

// TestAdminDLQList: the DLQ list returns the {events,total} envelope.
func TestAdminDLQList(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	resp := c.get("/v1/admin/dlq")
	requireStatus(t, resp, http.StatusOK)
	var body struct {
		Events []any `json:"events"`
		Total  int64 `json:"total"`
	}
	resp.decode(t, &body)
	if body.Total < 0 {
		t.Errorf("dlq total = %d, want >= 0", body.Total)
	}
}

// TestAdminQuotaUsage: quota usage reports the current billing period. It reads
// ClickHouse, so a 500 is treated as an upstream-unavailable skip.
func TestAdminQuotaUsage(t *testing.T) {
	c := apiClient(t)
	requireAdmin(t, c)

	resp := c.get("/v1/admin/quota/usage")
	if resp.Status == http.StatusInternalServerError {
		t.Skipf("quota usage query failed (ClickHouse unavailable? run `make ch-migrate`): %q", resp.errorMessage())
	}
	requireStatus(t, resp, http.StatusOK)
	var body struct {
		TenantID    string `json:"tenant_id"`
		Events      int64  `json:"events"`
		BytesStored int64  `json:"bytes_stored"`
	}
	resp.decode(t, &body)
	if body.TenantID == "" {
		t.Errorf("quota usage missing tenant_id: %s", truncate(resp.Body))
	}
}

// ── shared helpers ──────────────────────────────────────────────────────────

// idField decodes {"id": "..."} from a create response.
func idField(t *testing.T, resp *response) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	resp.decode(t, &body)
	if body.ID == "" {
		t.Fatalf("response has no id: %s", truncate(resp.Body))
	}
	return body.ID
}

// containsID reports whether a raw JSON body mentions the given resource id.
func containsID(body []byte, id string) bool {
	return id != "" && bytes.Contains(body, []byte(id))
}
