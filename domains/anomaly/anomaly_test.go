package anomaly

import "testing"

func TestScore(t *testing.T) {
	// No deviation → no score.
	if s := Score(10, 10, 3); s != 0 {
		t.Errorf("no-deviation score = %v, want 0", s)
	}
	// A drop below the mean is not an anomaly.
	if s := Score(2, 10, 3); s != 0 {
		t.Errorf("below-mean score = %v, want 0", s)
	}
	// A big spike out of a FLAT baseline (std=0) is still detected via the
	// Poisson √mean floor — the case that broke naive z-scoring.
	if s := Score(200, 2, 0); s < 0.9 {
		t.Errorf("spike from flat baseline score = %v, want >= 0.9", s)
	}
	// A large spike over a noisy baseline → full score.
	if s := Score(200, 5, 2); s != 1.0 {
		t.Errorf("large spike score = %v, want 1.0", s)
	}
	// A modest bump → partial score in (0,1).
	if s := Score(20, 10, 3); s <= 0 || s >= 1 {
		t.Errorf("modest bump score = %v, want in (0,1)", s)
	}
}
