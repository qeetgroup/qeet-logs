package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/routingsim"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// routingSimRequest is the POST /v1/alerts/simulate body.
type routingSimRequest struct {
	Service  string            `json:"service"`
	Severity string            `json:"severity"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// routingSimResponse summarises which alert-rule channels/targets would fire.
type routingSimResponse struct {
	Service       string               `json:"service"`
	Severity      string               `json:"severity"`
	Labels        map[string]string    `json:"labels,omitempty"`
	RuleCount     int                  `json:"rule_count"`
	MatchedCount  int                  `json:"matched_count"`
	Results       []routingsim.Result  `json:"results"`
	ChannelsFired []routingsim.Channel `json:"channels_fired"`
}

// SimulateAlertRouting handles POST /v1/alerts/simulate.
//
// It loads the authenticated tenant's alert rules and runs a pure matcher
// (domains/routingsim) to report which rules — and therefore which channels/
// targets — WOULD fire for a synthetic {service, severity, labels?} event,
// without querying ClickHouse or delivering anything (PRD 17.4 — Routing Rule
// Simulation).
//
// Scope: logs:read.
func SimulateAlertRouting(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read scope"})
			return
		}
		tenant := apimw.TenantID(ctx)

		var req routingSimRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if req.Service == "" && req.Severity == "" && len(req.Labels) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provide at least one of service, severity, labels"})
			return
		}

		rules, err := routingSimLoadRules(ctx, pool, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load alert rules"})
			return
		}

		results := routingsim.Simulate(routingsim.Event{
			Service:  req.Service,
			Severity: req.Severity,
			Labels:   req.Labels,
		}, rules)

		matched := 0
		fired := []routingsim.Channel{}
		seen := map[string]bool{}
		for _, res := range results {
			if !res.Matched {
				continue
			}
			matched++
			for _, c := range res.Channels {
				k := c.Type + "\x00" + c.Target
				if !seen[k] {
					seen[k] = true
					fired = append(fired, c)
				}
			}
		}

		writeJSON(w, http.StatusOK, routingSimResponse{
			Service:       req.Service,
			Severity:      req.Severity,
			Labels:        req.Labels,
			RuleCount:     len(rules),
			MatchedCount:  matched,
			Results:       results,
			ChannelsFired: fired,
		})
	}
}

// routingSimLoadRules loads the tenant's alert rules into the routing-relevant
// subset used by the pure matcher. Mirrors handler/alerts.go ListAlertRules'
// query (tenant scoped by identity), decoding the channels JSONB column.
func routingSimLoadRules(ctx context.Context, pool *pgxpool.Pool, tenant string) ([]routingsim.Rule, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, kind, service, condition, channels, enabled
		FROM alert_rules
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []routingsim.Rule
	for rows.Next() {
		var (
			id, name, kind string
			service, cond  *string
			rawCh          []byte
			enabled        bool
		)
		if err := rows.Scan(&id, &name, &kind, &service, &cond, &rawCh, &enabled); err != nil {
			return nil, err
		}
		var chans []routingsim.Channel
		if len(rawCh) > 0 {
			_ = json.Unmarshal(rawCh, &chans)
		}
		rule := routingsim.Rule{
			ID:       id,
			Name:     name,
			Kind:     kind,
			Enabled:  enabled,
			Channels: chans,
		}
		if service != nil {
			rule.Service = *service
		}
		if cond != nil {
			rule.Condition = *cond
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}
