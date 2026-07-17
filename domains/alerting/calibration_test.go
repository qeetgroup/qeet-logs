package alerting

import "testing"

func TestCalibrationFactor(t *testing.T) {
	// Below the sample floor → neutral, regardless of ratio.
	if f := calibrationFactor(1, 0); f != 1.0 {
		t.Errorf("cold start (1 sample) = %v, want neutral 1.0", f)
	}
	if f := calibrationFactor(0, 3); f != 1.0 {
		t.Errorf("cold start (3 samples) = %v, want neutral 1.0", f)
	}

	// All-noise (enough samples) → damped to the floor 0.5.
	if f := calibrationFactor(0, 10); f != 0.5 {
		t.Errorf("all-noise = %v, want 0.5", f)
	}
	// All-actionable → slight boost 1.15.
	if f := calibrationFactor(10, 0); f < 1.14 || f > 1.16 {
		t.Errorf("all-actionable = %v, want ~1.15", f)
	}
	// Half-and-half → midpoint ~0.825.
	f := calibrationFactor(5, 5)
	if f < 0.82 || f > 0.83 {
		t.Errorf("50/50 = %v, want ~0.825", f)
	}

	// A noisy service damps confidence below an equally-scored clean service:
	// a 0.7 raw score stays a page (>=0.6) when actionable, but drops below the
	// gate once the service is known to be mostly noise.
	clean := 0.7 * calibrationFactor(8, 2) // ratio 0.8 → 1.02
	noisy := 0.7 * calibrationFactor(2, 8) // ratio 0.2 → 0.63
	if !(clean > 0.6 && noisy < 0.6) {
		t.Errorf("expected clean>gate>noisy: clean=%.3f noisy=%.3f", clean, noisy)
	}
}
