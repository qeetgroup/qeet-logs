package alerting

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/qeetgroup/qeet-logs/domains/topology"
	"github.com/qeetgroup/qeet-logs/domains/webhook"
)

// Fingerprint is the correlation key that collapses signals for one service
// into a single incident (PRD Module 13.2). Kept deliberately coarse — one open
// incident per (tenant, service) — so repeated firings and multiple rules for
// the same service dedup rather than paging separately.
func Fingerprint(tenantID, service string) string {
	h := sha1.Sum([]byte(tenantID + "|" + service))
	return hex.EncodeToString(h[:])
}

func serviceOf(rule AlertRule) string {
	if rule.Service != nil && *rule.Service != "" {
		return *rule.Service
	}
	return "*"
}

// correlate upserts the open incident for a firing rule, deduping repeated
// firings, and pages exactly once when the merged confidence crosses the page
// threshold. Below the threshold the incident stays in the low-severity feed.
func (e *Engine) correlate(ctx context.Context, rule AlertRule, count int64, conf float64) error {
	svc := serviceOf(rule)
	fp := Fingerprint(rule.TenantID, svc)
	// Topology-proximity correlation (G11 tracked): if a topology-adjacent service
	// already has an open incident, merge into that one rather than opening a new
	// incident for the same degradation propagating through the graph.
	if proxyFP := e.neighborIncidentFP(ctx, rule.TenantID, svc); proxyFP != "" {
		fp = proxyFP
	}
	deployID := e.nearbyDeploy(ctx, rule.TenantID, svc, rule.WindowSeconds)
	title := fmt.Sprintf("%s degraded", svc)
	if svc == "*" {
		title = rule.Name
	}

	// Continuous calibration (Module 13.3): scale the raw confidence by what past
	// operator verdicts say about this service's signal quality before it reaches
	// the page gate. Neutral (1.0) until there's enough feedback.
	conf = clamp01(conf * e.CalibrationFactor(ctx, rule.TenantID, svc))

	var (
		id           string
		mergedConf   float64
		signalCount  int
		alreadyPaged bool
		newlyOpened  bool // xmax = 0 ⇒ this row was INSERTed, not merged into an existing incident
	)
	err := e.pool.QueryRow(ctx, `
		INSERT INTO incidents (tenant_id, fingerprint, title, service, severity, confidence,
		                       signal_count, deploy_id, correlated_rules)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, 1, NULLIF($7,''), jsonb_build_array($8::text))
		ON CONFLICT (fingerprint) WHERE status = 'open' DO UPDATE SET
		    signal_count = incidents.signal_count + 1,
		    last_seen    = now(),
		    confidence   = greatest(incidents.confidence, EXCLUDED.confidence),
		    deploy_id    = COALESCE(incidents.deploy_id, EXCLUDED.deploy_id),
		    correlated_rules = CASE
		        WHEN incidents.correlated_rules @> jsonb_build_array($8::text)
		        THEN incidents.correlated_rules
		        ELSE incidents.correlated_rules || jsonb_build_array($8::text)
		    END
		RETURNING id, confidence, signal_count, paged, (xmax = 0)`,
		rule.TenantID, fp, title, svc, Severity(conf), conf, deployID, rule.ID,
	).Scan(&id, &mergedConf, &signalCount, &alreadyPaged, &newlyOpened)
	if err != nil {
		return fmt.Errorf("upsert incident: %w", err)
	}

	// Severity tracks the merged confidence.
	sev := Severity(mergedConf)
	if _, err := e.pool.Exec(ctx, `UPDATE incidents SET severity = $1 WHERE id = $2::uuid`, sev, id); err != nil {
		return fmt.Errorf("update severity: %w", err)
	}

	// Fire the outbound `incident.opened` webhook once, when the incident is first
	// opened (Module 30.4). Best-effort + detached so slow receivers never block
	// or fail the alerter cycle.
	if newlyOpened {
		e.dispatchWebhook(rule.TenantID, "incident.opened", map[string]any{
			"event": "incident.opened", "incident_id": id, "tenant_id": rule.TenantID,
			"service": svc, "severity": sev, "confidence": mergedConf, "title": title,
			"deploy_id": deployID, "at": time.Now().UTC(),
		})
	}

	// Page exactly once, and only at/above the confidence gate.
	if !alreadyPaged && mergedConf >= e.pageMinConfidence {
		payload := Payload{
			AlertID:   id,
			AlertName: title,
			TenantID:  rule.TenantID,
			Kind:      rule.Kind,
			Firing:    true,
			Count:     count,
			FiredAt:   time.Now().UTC(),
			Message: fmt.Sprintf("[%s conf=%.2f] %s — %d correlated signal(s)%s",
				sev, mergedConf, title, signalCount, deploySuffix(deployID)),
		}
		if err := Deliver(ctx, rule, payload, e.notifyURL, e.notifyKey); err != nil {
			e.log.Error().Err(err).Str("incident", id).Msg("deliver page")
		}
		if _, err := e.pool.Exec(ctx, `UPDATE incidents SET paged = true WHERE id = $1::uuid`, id); err != nil {
			return fmt.Errorf("mark paged: %w", err)
		}
		e.log.Warn().Str("incident", id).Str("service", svc).Str("severity", sev).
			Float64("confidence", mergedConf).Msg("incident paged")
	} else if !alreadyPaged {
		e.log.Info().Str("incident", id).Str("service", svc).Float64("confidence", mergedConf).
			Msg("below page threshold → low-severity feed")
	}
	return nil
}

// resolveOpenIncident closes the open incident for a rule's service.
func (e *Engine) resolveOpenIncident(ctx context.Context, rule AlertRule) error {
	svc := serviceOf(rule)
	fp := Fingerprint(rule.TenantID, svc)
	tag, err := e.pool.Exec(ctx,
		`UPDATE incidents SET status = 'resolved', resolved_at = now()
		 WHERE fingerprint = $1 AND status = 'open'`, fp)
	if err == nil && tag.RowsAffected() > 0 {
		e.dispatchWebhook(rule.TenantID, "incident.resolved", map[string]any{
			"event": "incident.resolved", "tenant_id": rule.TenantID,
			"service": svc, "at": time.Now().UTC(),
		})
	}
	return err
}

// dispatchWebhook fires an outbound webhook in the background on a detached
// context, so a slow or down receiver never blocks or cancels the alerter cycle
// (PRD Module 30.4). Delivery is best-effort — see domains/webhook.
func (e *Engine) dispatchWebhook(tenant, event string, payload map[string]any) {
	pool := e.pool
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		webhook.Dispatch(ctx, pool, tenant, event, payload)
	}()
}

// neighborIncidentFP looks up the topology graph and returns the fingerprint of
// an open incident for a 1-hop neighbor of service. Returns "" if none found.
// This collapses cascading failures (A calls B; B degrades → A fires too) into
// one incident rather than opening separate ones.
func (e *Engine) neighborIncidentFP(ctx context.Context, tenantID, service string) string {
	if service == "*" {
		return ""
	}
	g, err := topology.Derive(ctx, e.ch, tenantID, 3600)
	if err != nil || g == nil {
		return ""
	}
	neighbors := g.Neighbors(service)
	if len(neighbors) == 0 {
		return ""
	}
	fps := make([]string, 0, len(neighbors))
	for _, n := range neighbors {
		fps = append(fps, Fingerprint(tenantID, n))
	}
	var existingFP string
	err = e.pool.QueryRow(ctx,
		`SELECT fingerprint FROM incidents WHERE fingerprint = ANY($1::text[]) AND status = 'open' LIMIT 1`,
		fps,
	).Scan(&existingFP)
	if err != nil {
		return ""
	}
	return existingFP
}

// nearbyDeploy returns the most recent deploy for a service within the window
// (deploy-proximity correlation key, Module 13.2 / 15). Best-effort: "" on miss.
func (e *Engine) nearbyDeploy(ctx context.Context, tenant, service string, windowSec int) string {
	if service == "*" {
		return ""
	}
	sql := fmt.Sprintf(
		`SELECT deploy_id FROM change_events
		 WHERE tenant_id = '%s' AND service = '%s' AND kind = 'deploy'
		   AND timestamp > now() - INTERVAL %d SECOND
		 ORDER BY timestamp DESC LIMIT 1`,
		escapeSingle(tenant), escapeSingle(service), windowSec*3)
	rows, err := e.ch.Query(ctx, sql)
	if err != nil || len(rows) == 0 {
		return ""
	}
	if d, ok := rows[0]["deploy_id"].(string); ok {
		return d
	}
	return ""
}

func deploySuffix(deployID string) string {
	if deployID == "" {
		return ""
	}
	return " (near deploy " + deployID + ")"
}
