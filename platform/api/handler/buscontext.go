package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/buscontext"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// buscontext.go serves the Business Context Correlation Layer (PRD Module 16,
// gap P2-G4): admin CRUD over per-service business mappings (customer / plan-tier
// / revenue / SLA), plus read endpoints that turn those mappings into an
// incident-facing exposure estimate (affected plan tiers, a qualified
// revenue-at-risk range, strictest SLA target). Admin routes sit under the
// /v1/admin group (logs:admin enforced by RequireScope); read routes self-check
// logs:read (also accepting logs:query, matching the sibling read endpoints).
// Helpers are prefixed busContext to avoid collisions in the shared handler package.

// busContextRequireRead returns true (and writes 403) unless the caller may read.
func busContextRequireRead(w http.ResponseWriter, r *http.Request) bool {
	if !apimw.HasScope(r.Context(), "logs:read", "logs:query") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
		return false
	}
	return true
}

// BusContextCreate registers a business-context mapping (Module 16.1).
// POST /v1/admin/business-context {service, customer?, plan_tier?, monthly_revenue?, sla_target?, owner?, notes?}.
// Requires logs:admin (enforced by the /v1/admin route group).
func BusContextCreate(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var body struct {
			Service        string  `json:"service"`
			Customer       string  `json:"customer"`
			PlanTier       string  `json:"plan_tier"`
			MonthlyRevenue float64 `json:"monthly_revenue"`
			SLATarget      float64 `json:"sla_target"`
			Owner          string  `json:"owner"`
			Notes          string  `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.Service == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service is required"})
			return
		}
		row, err := buscontext.Create(r.Context(), pool, tenant, buscontext.Input{
			Service:        body.Service,
			Customer:       body.Customer,
			PlanTier:       body.PlanTier,
			MonthlyRevenue: body.MonthlyRevenue,
			SLATarget:      body.SLATarget,
			Owner:          body.Owner,
			Notes:          body.Notes,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, row)
	}
}

// BusContextList returns all business-context mappings for the tenant.
// GET /v1/admin/business-context. Requires logs:admin.
func BusContextList(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		rows, err := buscontext.LoadAll(r.Context(), pool, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(rows), "mappings": rows})
	}
}

// BusContextDelete removes a mapping. DELETE /v1/admin/business-context/{id}.
// Requires logs:admin.
func BusContextDelete(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		ok, err := buscontext.Delete(r.Context(), pool, tenant, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "business-context mapping not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// BusContextByService returns the mappings for a service plus their derived
// exposure. GET /v1/business-context?service=. Requires logs:read.
func BusContextByService(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !busContextRequireRead(w, r) {
			return
		}
		tenant := apimw.TenantID(r.Context())
		service := r.URL.Query().Get("service")
		if service == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service query parameter is required"})
			return
		}
		rows, err := buscontext.LoadByService(r.Context(), pool, tenant, service)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service":  service,
			"count":    len(rows),
			"mappings": rows,
			"exposure": buscontext.EstimateExposure(buscontext.ToMappings(rows)),
		})
	}
}

// BusContextForIncident tags an incident with its business exposure (Module 16.2).
// GET /v1/incidents/{id}/context — resolves the incident's service from Postgres,
// loads that service's mappings, and returns the exposure. Requires logs:read.
func BusContextForIncident(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !busContextRequireRead(w, r) {
			return
		}
		tenant := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")

		var service *string // incidents.service is nullable
		err := pool.QueryRow(r.Context(),
			`SELECT service FROM incidents WHERE id = $1::uuid AND tenant_id = $2::uuid`,
			id, tenant).Scan(&service)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		svc := ""
		if service != nil {
			svc = *service
		}
		rows, err := buscontext.LoadByService(r.Context(), pool, tenant, svc)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"incident_id": id,
			"service":     svc,
			"count":       len(rows),
			"mappings":    rows,
			"exposure":    buscontext.EstimateExposure(buscontext.ToMappings(rows)),
		})
	}
}
