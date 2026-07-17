package alerting

import "testing"

func TestConfidenceAndSeverity(t *testing.T) {
	// Absence is a strong signal.
	if c := Confidence(KindAbsence, 0, 0, nil); c != 0.7 {
		t.Errorf("absence confidence = %v, want 0.7", c)
	}
	// Cold start (no baseline): 2× threshold → full confidence.
	if c := Confidence(KindThreshold, 200, 100, nil); c != 1.0 {
		t.Errorf("2x threshold confidence = %v, want 1.0", c)
	}
	// Just over threshold → low confidence.
	if c := Confidence(KindThreshold, 110, 100, nil); c > 0.2 {
		t.Errorf("marginal breach confidence = %v, want small", c)
	}
	// Baseline z-score dominates when present: 6σ over mean → full confidence.
	b := &Baseline{Mean: 10, Std: 5, Windows: 12}
	if c := Confidence(KindThreshold, 40, 100, b); c != 1.0 {
		t.Errorf("6-sigma confidence = %v, want 1.0", c)
	}
	// Severity buckets.
	cases := map[float64]string{0.1: "low", 0.4: "medium", 0.7: "high", 0.95: "critical"}
	for conf, want := range cases {
		if got := Severity(conf); got != want {
			t.Errorf("Severity(%v) = %q, want %q", conf, got, want)
		}
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a := Fingerprint("t1", "payments")
	if a != Fingerprint("t1", "payments") {
		t.Error("fingerprint not stable")
	}
	if a == Fingerprint("t1", "gateway") || a == Fingerprint("t2", "payments") {
		t.Error("fingerprint collision across service/tenant")
	}
}
