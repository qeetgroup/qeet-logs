package alerting

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// Engine runs the alert evaluation loop. It polls Postgres for enabled rules,
// evaluates each against ClickHouse, and delivers notifications on transitions.
type Engine struct {
	pool      *pgxpool.Pool
	ch        *clickhouse.Client
	notifyURL string
	notifyKey string
	interval  time.Duration
	log       zerolog.Logger
}

// New constructs an Engine with a configurable poll interval (default 60s).
func New(pool *pgxpool.Pool, ch *clickhouse.Client, notifyURL, notifyKey string, interval time.Duration, log zerolog.Logger) *Engine {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Engine{pool: pool, ch: ch, notifyURL: notifyURL, notifyKey: notifyKey, interval: interval, log: log}
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
}

func (e *Engine) evalRule(ctx context.Context, rule AlertRule) error {
	count, nowFiring, err := Evaluate(ctx, e.ch, rule)
	if err != nil {
		return err
	}

	prev, err := e.loadState(ctx, rule.ID)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	now := time.Now().UTC()
	changed := prev.Firing != nowFiring

	if changed {
		var firedAt, resolvedAt *time.Time
		msg := ""
		if nowFiring {
			firedAt = &now
			if rule.Kind == KindAbsence {
				msg = fmt.Sprintf("No logs for %ds", rule.WindowSeconds)
			} else {
				msg = fmt.Sprintf("Log count %d exceeded threshold %.0f in %ds window", count, *rule.Threshold, rule.WindowSeconds)
			}
			e.log.Warn().Str("rule", rule.Name).Str("tenant", rule.TenantID).Int64("count", count).Msg("alert firing")
		} else {
			resolvedAt = &now
			msg = fmt.Sprintf("Alert resolved after %ds", rule.WindowSeconds)
			e.log.Info().Str("rule", rule.Name).Str("tenant", rule.TenantID).Msg("alert resolved")
		}

		payload := Payload{
			AlertID:   rule.ID,
			AlertName: rule.Name,
			TenantID:  rule.TenantID,
			Kind:      rule.Kind,
			Firing:    nowFiring,
			Count:     count,
			Message:   msg,
		}
		if firedAt != nil {
			payload.FiredAt = *firedAt
		}

		if err := Deliver(ctx, rule, payload, e.notifyURL, e.notifyKey); err != nil {
			e.log.Error().Err(err).Str("rule", rule.Name).Msg("deliver alert")
		}

		if err := e.upsertState(ctx, AlertState{
			RuleID:     rule.ID,
			TenantID:   rule.TenantID,
			Firing:     nowFiring,
			FiredAt:    firedAt,
			ResolvedAt: resolvedAt,
			LastEval:   now,
		}); err != nil {
			return fmt.Errorf("upsert state: %w", err)
		}
	} else {
		// No state change — just update last_eval timestamp.
		if _, err := e.pool.Exec(ctx,
			`UPDATE alert_state SET last_eval = $1 WHERE rule_id = $2`,
			now, rule.ID,
		); err != nil {
			e.log.Warn().Err(err).Str("rule_id", rule.ID).Msg("update last_eval")
		}
	}
	return nil
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
