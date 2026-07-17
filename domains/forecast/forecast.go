// Package forecast is the Phase-2 Predictive Observability groundwork (PRD
// Module 14) — the always-on, honest STATISTICAL tier. It answers two
// operator questions from a per-window metric series, in pure Go, with no
// model to train, load, or drift:
//
//   - 14.1 Capacity / exhaustion forecasting: fit a least-squares trend and
//     project when a metric will cross a threshold (disk %, queue depth, error
//     rate, connection pool, …) — "you have ~N windows before this breaches".
//   - 14.2 Seasonal / deploy-aware trend: an EWMA smoothed level, a
//     seasonal-naive baseline (same phase, previous seasons), and the deviation
//     of the current value from that baseline — with an optional reset index so
//     a deploy that shifts a metric's behaviour doesn't poison the baseline
//     (pre-deploy history is discarded from the trend/baseline).
//
// Deliberately statistics-only. The heavier ONNX model tiers (PRD Module 14.3+
// — learned multivariate forecasters / probabilistic bands) are Phase-3 and
// remain GATED / out of scope here: this package adds NO ONNX/ML dependency and
// fakes no model. Like the Tier-1 anomaly scorer (domains/anomaly), this is the
// statistical floor every future predictive step builds on and can be compared
// against.
package forecast

import "math"

// LinearForecast fits an ordinary least-squares line y = slope*x + intercept to
// the series, treating x as the evenly-spaced window index 0,1,2,…,n-1. slope
// is the per-window rate of change (14.1). With <2 points there is no trend:
// slope is 0 and intercept is the single value (or 0 for an empty series).
func LinearForecast(points []float64) (slope, intercept float64) {
	n := len(points)
	if n == 0 {
		return 0, 0
	}
	if n == 1 {
		return 0, points[0]
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, y := range points {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	fn := float64(n)
	denom := fn*sumXX - sumX*sumX
	if denom == 0 { // all x equal — impossible for n>=2 distinct indices, but guard anyway
		return 0, sumY / fn
	}
	slope = (fn*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / fn
	return slope, intercept
}

// Project returns the fitted value `stepsAhead` windows past the last observed
// point, given a fit over `n` points (last index n-1). Used for the horizon
// projection reported by the API.
func Project(n int, slope, intercept float64, stepsAhead int) float64 {
	if n <= 0 {
		return intercept
	}
	x := float64(n-1) + float64(stepsAhead)
	return slope*x + intercept
}

// TimeToThreshold projects how many windows until `current`, moving at `slope`
// per window, crosses `threshold` (14.1 capacity/exhaustion). It works for both
// a ceiling (value rising toward a cap, e.g. disk 100%) and a floor (value
// falling toward a limit, e.g. free connections → 0):
//
//   - already at/past the threshold → steps 0, willBreach true;
//   - slope points toward the threshold → finite steps >= 0, willBreach true;
//   - slope is flat or points away → steps +Inf, willBreach false.
func TimeToThreshold(current, slope, threshold float64) (steps float64, willBreach bool) {
	gap := threshold - current
	if gap == 0 {
		return 0, true // sitting exactly on the threshold
	}
	// A breach needs the slope to move in the same direction as the gap.
	if slope == 0 || (gap > 0) != (slope > 0) {
		return math.Inf(1), false
	}
	return gap / slope, true
}

// EWMA returns the exponentially-weighted moving average (smoothed level) of the
// series: s_0 = x_0, s_t = alpha*x_t + (1-alpha)*s_{t-1}. Higher alpha tracks
// recent values faster. alpha is clamped to (0,1]. Empty series → 0. This is the
// smoothed "current level" for 14.2 trend reporting. Combine with AfterReset for
// a deploy-aware level.
func EWMA(points []float64, alpha float64) float64 {
	if len(points) == 0 {
		return 0
	}
	if alpha <= 0 {
		alpha = 0.3
	}
	if alpha > 1 {
		alpha = 1
	}
	s := points[0]
	for _, x := range points[1:] {
		s = alpha*x + (1-alpha)*s
	}
	return s
}

// AfterReset returns the sub-series at/after resetIdx — the deploy-aware slice.
// Pass the window index of the most recent deploy so trend/level are computed
// from post-deploy data only (14.2 deploy-aware baselining). A negative or
// out-of-range resetIdx yields the full series unchanged.
func AfterReset(points []float64, resetIdx int) []float64 {
	if resetIdx <= 0 || resetIdx >= len(points) {
		return points
	}
	return points[resetIdx:]
}

// SeasonalBaseline is a seasonal-naive expected value for the window right after
// the series: the mean of the values observed one, two, … full periods back
// (same phase, previous seasons). `period` is the season length in windows
// (e.g. 24 for hourly buckets over a daily cycle). resetIdx discards pre-deploy
// history so a behaviour shift doesn't poison the baseline (see AfterReset;
// pass <=0 for the full series). ok is false when less than one full period of
// post-reset history is available — in that case, WITHHOLD rather than guess
// (mirrors the anomaly scorer's insufficient-history contract).
func SeasonalBaseline(points []float64, period, resetIdx int) (baseline float64, ok bool) {
	if period <= 0 {
		return 0, false
	}
	start := 0
	if resetIdx > 0 {
		start = resetIdx
	}
	n := len(points)
	var sum float64
	var cnt int
	for i := n - period; i >= start; i -= period {
		sum += points[i]
		cnt++
	}
	if cnt == 0 {
		return 0, false
	}
	return sum / float64(cnt), true
}

// Deviation is the signed relative deviation of an actual value from an expected
// baseline: (actual-baseline)/max(|baseline|,1). >0 means above baseline, <0
// below. The unit floor of 1 keeps the ratio well-behaved for count/rate data
// with a near-zero baseline (same Poisson-ish guard the anomaly scorer uses),
// so a spike out of a flat-zero baseline still reads as a large deviation rather
// than dividing by ~0.
func Deviation(actual, baseline float64) float64 {
	denom := math.Abs(baseline)
	if denom < 1 {
		denom = 1
	}
	return (actual - baseline) / denom
}
