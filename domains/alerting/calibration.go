package alerting

import "context"

// Continuous calibration against resolution outcomes (PRD Module 13.3). Operator
// verdicts on resolved incidents (actionable vs noise, stored in incident_feedback)
// train a per-(tenant, service) confidence multiplier that scales future scores
// before the page gate — damping noisy services and preserving genuinely
// actionable ones. Below a minimum sample size the factor is neutral (1.0), so a
// cold-start service is never penalised.

const (
	// minCalibrationSamples is how many verdicts a (tenant, service) needs before
	// its feedback moves the multiplier off neutral.
	minCalibrationSamples = 5
	// calibrationWindowDays bounds how far back feedback counts (recent behaviour).
	calibrationWindowDays = 90
)

// calibrationFactor maps actionable/noise verdict counts to a confidence
// multiplier in [0.5, 1.15]: all-noise halves confidence (suppress pages),
// all-actionable gives a slight boost. Pure; unit-tested.
func calibrationFactor(actionable, noise int) float64 {
	total := actionable + noise
	if total < minCalibrationSamples {
		return 1.0
	}
	ratio := float64(actionable) / float64(total)
	return 0.5 + 0.65*ratio
}

// CalibrationFactor returns the confidence multiplier for a (tenant, service),
// derived from recent operator feedback. Any error → neutral 1.0 (calibration
// must never make the alerter fail).
func (e *Engine) CalibrationFactor(ctx context.Context, tenant, service string) float64 {
	if service == "" || service == "*" {
		return 1.0
	}
	var actionable, noise int
	err := e.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE verdict = 'actionable'),
		  count(*) FILTER (WHERE verdict = 'noise')
		FROM incident_feedback
		WHERE tenant_id = $1::uuid AND service = $2
		  AND created_at > now() - make_interval(days => $3)`,
		tenant, service, calibrationWindowDays,
	).Scan(&actionable, &noise)
	if err != nil {
		return 1.0
	}
	return calibrationFactor(actionable, noise)
}
