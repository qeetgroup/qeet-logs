package deploy

import "testing"

func TestNewHealthDelta(t *testing.T) {
	// Error rate rose 2%→12% after the change → degraded.
	h := newHealthDelta(1000, 20, 1000, 120)
	if h.BeforeRate != 0.02 || h.AfterRate != 0.12 {
		t.Fatalf("rates = %v/%v, want 0.02/0.12", h.BeforeRate, h.AfterRate)
	}
	if h.Delta != 0.10 {
		t.Errorf("delta = %v, want 0.10", h.Delta)
	}
	if !h.Degraded {
		t.Error("expected degraded=true for a 10-point error-rate rise")
	}

	// Stable error rate → not degraded.
	stable := newHealthDelta(1000, 50, 1000, 51)
	if stable.Degraded {
		t.Errorf("expected not degraded for a 0.1-point change, delta=%v", stable.Delta)
	}

	// No post-change telemetry → not degraded (can't correlate).
	none := newHealthDelta(500, 10, 0, 0)
	if none.Degraded || none.AfterRate != 0 {
		t.Errorf("expected not-degraded/zero after-rate with no after telemetry: %+v", none)
	}
}

func TestScoreCulprit(t *testing.T) {
	const window = 3600

	// A recent deploy that degraded health should outrank an equally recent
	// flag change with stable health.
	degraded := newHealthDelta(1000, 20, 1000, 220) // 2%→22%
	deployScore, reason := scoreCulprit("deploy", 60, window, degraded)
	if deployScore <= 0 || deployScore > 1 {
		t.Fatalf("deploy score out of range: %v", deployScore)
	}
	if reason == "" {
		t.Error("expected a human-readable reason")
	}

	stable := newHealthDelta(1000, 50, 1000, 50)
	flagScore, _ := scoreCulprit("flag", 60, window, stable)
	if deployScore <= flagScore {
		t.Errorf("degraded deploy (%.2f) should outrank stable flag (%.2f)", deployScore, flagScore)
	}

	// Recency: an old change scores lower than a fresh one (same kind, no health).
	fresh, _ := scoreCulprit("deploy", 30, window, nil)
	old, _ := scoreCulprit("deploy", 3000, window, nil)
	if fresh <= old {
		t.Errorf("fresh deploy (%.2f) should outrank old deploy (%.2f)", fresh, old)
	}

	// Change-type weighting: deploy outranks config at the same recency (no health).
	dep, _ := scoreCulprit("deploy", 100, window, nil)
	cfg, _ := scoreCulprit("config", 100, window, nil)
	if dep <= cfg {
		t.Errorf("deploy (%.2f) should outweigh config (%.2f) at equal recency", dep, cfg)
	}
}
