package alerting

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"
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
	deployID := e.nearbyDeploy(ctx, rule.TenantID, svc, rule.WindowSeconds)
	title := fmt.Sprintf("%s degraded", svc)
	if svc == "*" {
		title = rule.Name
	}

	var (
		id           string
		mergedConf   float64
		signalCount  int
		alreadyPaged bool
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
		RETURNING id, confidence, signal_count, paged`,
		rule.TenantID, fp, title, svc, Severity(conf), conf, deployID, rule.ID,
	).Scan(&id, &mergedConf, &signalCount, &alreadyPaged)
	if err != nil {
		return fmt.Errorf("upsert incident: %w", err)
	}

	// Severity tracks the merged confidence.
	sev := Severity(mergedConf)
	if _, err := e.pool.Exec(ctx, `UPDATE incidents SET severity = $1 WHERE id = $2::uuid`, sev, id); err != nil {
		return fmt.Errorf("update severity: %w", err)
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
	fp := Fingerprint(rule.TenantID, serviceOf(rule))
	_, err := e.pool.Exec(ctx,
		`UPDATE incidents SET status = 'resolved', resolved_at = now()
		 WHERE fingerprint = $1 AND status = 'open'`, fp)
	return err
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
