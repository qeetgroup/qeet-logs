package alerting

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/qeetgroup/qeet-logs/domains/anomaly"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

const (
	// baselineWindows is how many prior windows the adaptive baseline / anomaly
	// scorer averages over.
	baselineWindows = 12
	// anomalyWindowSec is the per-window size for the anomaly error-rate sweep.
	anomalyWindowSec = 300
	// anomalyMinScore is the floor an anomaly must clear to raise a signal.
	anomalyMinScore = 0.5
)

// Engine runs the alert evaluation loop. It polls Postgres for enabled rules,
// evaluates each against ClickHouse, correlates firings into incidents with a
// calibrated confidence, and pages only above the confidence gate.
type Engine struct {
	pool              *pgxpool.Pool
	ch                *clickhouse.Client
	notifyURL         string
	notifyKey         string
	interval          time.Duration
	pageMinConfidence float64
	log               zerolog.Logger
}

// New constructs an Engine with a configurable poll interval (default 60s). The
// page confidence gate is read from ALERT_PAGE_MIN_CONFIDENCE (default 0.6).
func New(pool *pgxpool.Pool, ch *clickhouse.Client, notifyURL, notifyKey string, interval time.Duration, log zerolog.Logger) *Engine {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	pageMin := 0.6
	if v, err := strconv.ParseFloat(os.Getenv("ALERT_PAGE_MIN_CONFIDENCE"), 64); err == nil && v >= 0 && v <= 1 {
		pageMin = v
	}
	return &Engine{pool: pool, ch: ch, notifyURL: notifyURL, notifyKey: notifyKey, interval: interval, pageMinConfidence: pageMin, log: log}
}

// Run starts the evaluation loop and blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	e.log.Info().Dur("interval", e.interval).Msg("alerter engine started")
	tick := time.NewTicker(e.interval)
	defer tick.Stop()

	// Evaluate immediately on startup, then on each tick.
	e.runCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			e.log.Info().Msg("alerter engine stopped")
			return
		case <-tick.C:
			e.runCycle(ctx)
		}
	}
}

func (e *Engine) runCycle(ctx context.Context) {
	rules, err := e.loadRules(ctx)
	if err != nil {
		e.log.Error().Err(err).Msg("load alert rules")
		return
	}
	for _, rule := range rules {
		if err := e.evalRule(ctx, rule); err != nil {
			e.log.Error().Err(err).Str("rule_id", rule.ID).Msg("evaluate rule")
		}
	}
	e.sweepAnomalies(ctx)
}

// sweepAnomalies runs the Tier-1 baseline anomaly scorer (G12) and feeds any
// outliers into the same correlation path as rules, so a spike raises/joins an
// incident even without a hand-configured threshold rule.
func (e *Engine) sweepAnomalies(ctx context.Context) {
	anomalies, err := anomaly.Sweep(ctx, e.ch, anomalyWindowSec, baselineWindows, anomalyMinScore)
	if err != nil {
		e.log.Warn().Err(err).Msg("anomaly sweep")
		return
	}
	for _, a := range anomalies {
		svc := a.Service
		rule := AlertRule{
			ID:            "anomaly:" + a.Service,
			TenantID:      a.TenantID,
			Name:          "anomaly: " + a.Service,
			Kind:          KindAnomaly,
			Service:       &svc,
			WindowSeconds: anomalyWindowSec,
		}
		// The anomaly score is carried in as the confidence via threshOf → Confidence.
		conf := Confidence(KindAnomaly, 0, a.Score, nil)
		if err := e.correlate(ctx, rule, int64(a.Current), conf); err != nil {
			e.log.Error().Err(err).Str("service", a.Service).Msg("correlate anomaly")
		}
	}
}

func (e *Engine) evalRule(ctx context.Context, rule AlertRule) error {
	count, staticFiring, err := Evaluate(ctx, e.ch, rule)
	if err != nil {
		return err
	}

	// Adaptive baselines over static thresholds (Module 13.4): a threshold rule
	// also fires when the count is a strong outlier vs its rolling baseline, and
	// the baseline sharpens the confidence score. Static threshold is the
	// cold-start fallback when there isn't enough history.
	base, err := ComputeBaseline(ctx, e.ch, rule, baselineWindows)
	if err != nil {
		e.log.Warn().Err(err).Str("rule_id", rule.ID).Msg("baseline (using static)")
	}
	adaptiveFiring := base != nil && base.Std > 0 && float64(count) > base.Mean+3*base.Std
	nowFiring := staticFiring || adaptiveFiring

	prev, err := e.loadState(ctx, rule.ID)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	now := time.Now().UTC()

	// While firing, correlate into an incident every cycle — this dedups repeated
	// firings and pages once, above the confidence gate (Module 13.1/13.2).
	if nowFiring {
		conf := Confidence(rule.Kind, count, threshOf(rule), base)
		if err := e.correlate(ctx, rule, count, conf); err != nil {
			e.log.Error().Err(err).Str("rule", rule.Name).Msg("correlate incident")
		}
	}

	changed := prev.Firing != nowFiring
	if changed {
		var firedAt, resolvedAt *time.Time
		if nowFiring {
			firedAt = &now
			e.log.Warn().Str("rule", rule.Name).Str("tenant", rule.TenantID).
				Int64("count", count).Bool("adaptive", adaptiveFiring).Msg("alert firing")
		} else {
			resolvedAt = &now
			// Close the correlated incident and deliver a resolve notification.
			if err := e.resolveOpenIncident(ctx, rule); err != nil {
				e.log.Warn().Err(err).Str("rule", rule.Name).Msg("resolve incident")
			}
			payload := Payload{
				AlertID: rule.ID, AlertName: rule.Name, TenantID: rule.TenantID,
				Kind: rule.Kind, Firing: false, Count: count,
				Message: fmt.Sprintf("Alert resolved after %ds", rule.WindowSeconds),
			}
			if err := Deliver(ctx, rule, payload, e.notifyURL, e.notifyKey); err != nil {
				e.log.Error().Err(err).Str("rule", rule.Name).Msg("deliver resolve")
			}
			e.log.Info().Str("rule", rule.Name).Str("tenant", rule.TenantID).Msg("alert resolved")
		}

		if err := e.upsertState(ctx, AlertState{
			RuleID: rule.ID, TenantID: rule.TenantID, Firing: nowFiring,
			FiredAt: firedAt, ResolvedAt: resolvedAt, LastEval: now,
		}); err != nil {
			return fmt.Errorf("upsert state: %w", err)
		}
	} else {
		if _, err := e.pool.Exec(ctx,
			`UPDATE alert_state SET last_eval = $1 WHERE rule_id = $2`,
			now, rule.ID,
		); err != nil {
			e.log.Warn().Err(err).Str("rule_id", rule.ID).Msg("update last_eval")
		}
	}
	return nil
}

// threshOf returns the static threshold (0 when unset, e.g. absence rules).
func threshOf(rule AlertRule) float64 {
	if rule.Threshold != nil {
		return *rule.Threshold
	}
	return 0
}

// loadRules fetches all enabled alert rules across all tenants.
func (e *Engine) loadRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT id, tenant_id, name, kind, service, condition, threshold,
		       window_seconds, channels, enabled, created_at
		FROM alert_rules
		WHERE enabled = true
		ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var r AlertRule
		var rawCh []byte
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.Name, &r.Kind,
			&r.Service, &r.Condition, &r.Threshold,
			&r.WindowSeconds, &rawCh, &r.Enabled, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Channels, _ = decodeChannels(rawCh)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// loadState retrieves the current alert_state for a rule, returning a zeroed
// state (not firing) if no row exists yet.
func (e *Engine) loadState(ctx context.Context, ruleID string) (AlertState, error) {
	var s AlertState
	err := e.pool.QueryRow(ctx,
		`SELECT rule_id, tenant_id, firing, fired_at, resolved_at, last_eval
		 FROM alert_state WHERE rule_id = $1`,
		ruleID,
	).Scan(&s.RuleID, &s.TenantID, &s.Firing, &s.FiredAt, &s.ResolvedAt, &s.LastEval)
	if err != nil && err.Error() == "no rows in result set" {
		return AlertState{RuleID: ruleID, Firing: false}, nil
	}
	return s, err
}

// upsertState inserts or replaces the alert_state row for a rule.
func (e *Engine) upsertState(ctx context.Context, s AlertState) error {
	_, err := e.pool.Exec(ctx, `
		INSERT INTO alert_state (rule_id, tenant_id, firing, fired_at, resolved_at, last_eval)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (rule_id) DO UPDATE SET
		    firing = EXCLUDED.firing,
		    fired_at = COALESCE(EXCLUDED.fired_at, alert_state.fired_at),
		    resolved_at = EXCLUDED.resolved_at,
		    last_eval = EXCLUDED.last_eval
	`, s.RuleID, s.TenantID, s.Firing, s.FiredAt, s.ResolvedAt, s.LastEval)
	return err
}
