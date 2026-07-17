package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/postmortem"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// postmortem.go serves incident postmortems + remediation commitments (PRD
// Module 20) and the CERT-In 6-hour incident export (PRD Module 27.2). All
// routes mount under /v1/admin and therefore require the logs:admin scope
// (enforced by the RequireScope middleware on that route group). Every query is
// scoped by tenant_id from the authenticated identity — never from user input.

// postmortemSelectSQL is the shared column projection for postmortem reads.
const postmortemSelectSQL = `
	SELECT id, tenant_id, incident_id, title, summary, timeline, root_cause,
	       impact, status, created_at, updated_at, published_at
	FROM postmortems`

type postmortemRow struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	IncidentID  *string    `json:"incident_id"`
	Title       string     `json:"title"`
	Summary     *string    `json:"summary"`
	Timeline    *string    `json:"timeline"`
	RootCause   *string    `json:"root_cause"`
	Impact      *string    `json:"impact"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	PublishedAt *time.Time `json:"published_at"`
}

type postmortemCommitmentRow struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	PostmortemID string     `json:"postmortem_id"`
	Description  string     `json:"description"`
	DueDate      *time.Time `json:"due_date"`
	AlertRuleID  *string    `json:"alert_rule_id"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
}

func postmortemScan(row interface {
	Scan(...any) error
}) (postmortemRow, error) {
	var p postmortemRow
	err := row.Scan(&p.ID, &p.TenantID, &p.IncidentID, &p.Title, &p.Summary,
		&p.Timeline, &p.RootCause, &p.Impact, &p.Status, &p.CreatedAt,
		&p.UpdatedAt, &p.PublishedAt)
	return p, err
}

func postmortemScanCommitment(row interface {
	Scan(...any) error
}) (postmortemCommitmentRow, error) {
	var c postmortemCommitmentRow
	err := row.Scan(&c.ID, &c.TenantID, &c.PostmortemID, &c.Description,
		&c.DueDate, &c.AlertRuleID, &c.Status, &c.CreatedAt)
	return c, err
}

// CreatePostmortem handles POST /v1/admin/postmortems.
func CreatePostmortem(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		var body struct {
			IncidentID *string `json:"incident_id"`
			Title      string  `json:"title"`
			Summary    *string `json:"summary"`
			Timeline   *string `json:"timeline"`
			RootCause  *string `json:"root_cause"`
			Impact     *string `json:"impact"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
			return
		}
		row := pool.QueryRow(r.Context(), `
			INSERT INTO postmortems (tenant_id, incident_id, title, summary, timeline, root_cause, impact)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)
			RETURNING id, tenant_id, incident_id, title, summary, timeline, root_cause,
			          impact, status, created_at, updated_at, published_at
		`, tenantID, body.IncidentID, body.Title, body.Summary, body.Timeline, body.RootCause, body.Impact)
		pm, err := postmortemScan(row)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, pm)
	}
}

// ListPostmortems handles GET /v1/admin/postmortems.
func ListPostmortems(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		rows, err := pool.Query(r.Context(), postmortemSelectSQL+`
			WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		results := []postmortemRow{}
		for rows.Next() {
			pm, err := postmortemScan(rows)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			results = append(results, pm)
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// GetPostmortem handles GET /v1/admin/postmortems/{id}.
func GetPostmortem(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		row := pool.QueryRow(r.Context(), postmortemSelectSQL+`
			WHERE id = $1 AND tenant_id = $2`, id, tenantID)
		pm, err := postmortemScan(row)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "postmortem not found"})
			return
		}
		writeJSON(w, http.StatusOK, pm)
	}
}

// UpdatePostmortem handles PATCH /v1/admin/postmortems/{id}. Only supplied
// fields change (COALESCE keeps the rest); publishing stamps published_at once.
func UpdatePostmortem(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		var body struct {
			Title     *string `json:"title"`
			Summary   *string `json:"summary"`
			Timeline  *string `json:"timeline"`
			RootCause *string `json:"root_cause"`
			Impact    *string `json:"impact"`
			Status    *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Status != nil && *body.Status != "draft" && *body.Status != "published" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be draft or published"})
			return
		}
		row := pool.QueryRow(r.Context(), `
			UPDATE postmortems SET
				title        = COALESCE($1, title),
				summary      = COALESCE($2, summary),
				timeline     = COALESCE($3, timeline),
				root_cause   = COALESCE($4, root_cause),
				impact       = COALESCE($5, impact),
				status       = COALESCE($6, status),
				published_at = CASE
					WHEN COALESCE($6, status) = 'published' AND published_at IS NULL THEN now()
					ELSE published_at
				END,
				updated_at   = now()
			WHERE id = $7 AND tenant_id = $8
			RETURNING id, tenant_id, incident_id, title, summary, timeline, root_cause,
			          impact, status, created_at, updated_at, published_at
		`, body.Title, body.Summary, body.Timeline, body.RootCause, body.Impact, body.Status, id, tenantID)
		pm, err := postmortemScan(row)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "postmortem not found"})
			return
		}
		writeJSON(w, http.StatusOK, pm)
	}
}

// CreatePostmortemCommitment handles POST /v1/admin/postmortems/{id}/commitments.
func CreatePostmortemCommitment(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		pmID := chi.URLParam(r, "id")
		var body struct {
			Description string  `json:"description"`
			DueDate     *string `json:"due_date"`
			AlertRuleID *string `json:"alert_rule_id"`
			Status      *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Description == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description required"})
			return
		}
		if body.Status != nil && *body.Status != "open" && *body.Status != "done" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be open or done"})
			return
		}
		status := "open"
		if body.Status != nil {
			status = *body.Status
		}
		// The parent postmortem must belong to this tenant.
		var exists bool
		if err := pool.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM postmortems WHERE id = $1 AND tenant_id = $2)`,
			pmID, tenantID).Scan(&exists); err != nil || !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "postmortem not found"})
			return
		}
		row := pool.QueryRow(r.Context(), `
			INSERT INTO remediation_commitments (tenant_id, postmortem_id, description, due_date, alert_rule_id, status)
			VALUES ($1, $2, $3, $4::date, $5::uuid, $6)
			RETURNING id, tenant_id, postmortem_id, description, due_date, alert_rule_id, status, created_at
		`, tenantID, pmID, body.Description, body.DueDate, body.AlertRuleID, status)
		c, err := postmortemScanCommitment(row)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

// ListPostmortemCommitments handles GET /v1/admin/postmortems/{id}/commitments.
func ListPostmortemCommitments(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		pmID := chi.URLParam(r, "id")
		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, postmortem_id, description, due_date, alert_rule_id, status, created_at
			FROM remediation_commitments
			WHERE postmortem_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`, pmID, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		results := []postmortemCommitmentRow{}
		for rows.Next() {
			c, err := postmortemScanCommitment(rows)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			results = append(results, c)
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// ExportPostmortemCERTIn handles GET /v1/admin/postmortems/{id}/cert-in-export.
// It joins the postmortem with its linked incident row and returns the CERT-In
// 6-hour incident report (PRD Module 27.2).
func ExportPostmortemCERTIn(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		row := pool.QueryRow(r.Context(), postmortemSelectSQL+`
			WHERE id = $1 AND tenant_id = $2`, id, tenantID)
		pm, err := postmortemScan(row)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "postmortem not found"})
			return
		}
		inc := postmortemLoadIncident(r.Context(), pool, tenantID, pm.IncidentID)
		report := postmortem.BuildCERTInExport(postmortemToDomain(pm), inc)
		writeJSON(w, http.StatusOK, report)
	}
}

// postmortemLoadIncident best-effort loads the linked incident row. A missing
// or unreadable incident yields the zero IncidentMeta so the export still
// returns a valid (if sparse) report.
func postmortemLoadIncident(ctx context.Context, pool *pgxpool.Pool, tenantID string, incidentID *string) postmortem.IncidentMeta {
	if incidentID == nil || *incidentID == "" {
		return postmortem.IncidentMeta{}
	}
	var (
		inc                       postmortem.IncidentMeta
		service, severity, status *string
	)
	err := pool.QueryRow(ctx, `
		SELECT id, service, severity, status, first_seen, resolved_at
		FROM incidents WHERE id = $1 AND tenant_id = $2
	`, *incidentID, tenantID).Scan(&inc.ID, &service, &severity, &status, &inc.FirstSeen, &inc.ResolvedAt)
	if err != nil {
		return postmortem.IncidentMeta{}
	}
	inc.Service = postmortemStr(service)
	inc.Severity = postmortemStr(severity)
	inc.Status = postmortemStr(status)
	return inc
}

func postmortemToDomain(p postmortemRow) postmortem.Postmortem {
	return postmortem.Postmortem{
		ID:          p.ID,
		TenantID:    p.TenantID,
		IncidentID:  postmortemStr(p.IncidentID),
		Title:       p.Title,
		Summary:     postmortemStr(p.Summary),
		Timeline:    postmortemStr(p.Timeline),
		RootCause:   postmortemStr(p.RootCause),
		Impact:      postmortemStr(p.Impact),
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		PublishedAt: p.PublishedAt,
	}
}

func postmortemStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
