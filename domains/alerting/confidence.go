package alerting

// Confidence scoring (PRD Module 13.1): every detector output earns a calibrated
// [0,1] score before it is allowed to page. Below the page threshold a signal
// routes to the low-severity incident feed instead of paging a human — the same
// confidence-gate discipline the RCA engine uses (Design Principle 2).

// KindAnomaly is a synthetic detector kind for baseline anomaly signals (G12):
// the anomaly score IS the confidence, fed straight into correlation.
const KindAnomaly = "anomaly"

// Baseline is a per-service/rule rolling statistic used for adaptive detection.
type Baseline struct {
	Mean    float64
	Std     float64
	Windows int
}

// Confidence maps a firing to a calibrated score in [0,1].
//   - absence: a strong, low-noise signal → fixed high-ish score.
//   - threshold with a baseline: z-score of the count vs the rolling baseline
//     (z=6 → full confidence).
//   - threshold without a baseline (cold start): excess over the static
//     threshold (2× threshold → full confidence).
func Confidence(kind string, count int64, threshold float64, base *Baseline) float64 {
	switch kind {
	case KindAnomaly:
		// The anomaly score is passed in via `threshold` (0..1) by the sweep.
		return clamp01(threshold)
	case KindAbsence:
		return 0.7
	case KindThreshold:
		if base != nil && base.Std > 0 {
			z := (float64(count) - base.Mean) / base.Std
			return clamp01(z / 6.0)
		}
		if threshold > 0 {
			return clamp01((float64(count) - threshold) / threshold)
		}
		return 0.5
	default:
		return 0.5
	}
}

// Severity buckets a confidence score into a human severity label.
func Severity(conf float64) string {
	switch {
	case conf >= 0.85:
		return "critical"
	case conf >= 0.6:
		return "high"
	case conf >= 0.3:
		return "medium"
	default:
		return "low"
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
