package forecast

import (
	"math"
	"testing"
)

func TestLinearForecastRising(t *testing.T) {
	// y = x + 1 → slope 1, intercept 1.
	slope, intercept := LinearForecast([]float64{1, 2, 3, 4, 5})
	if math.Abs(slope-1) > 1e-9 {
		t.Errorf("rising slope = %v, want 1", slope)
	}
	if math.Abs(intercept-1) > 1e-9 {
		t.Errorf("rising intercept = %v, want 1", intercept)
	}
	if slope <= 0 {
		t.Errorf("rising series must have positive slope, got %v", slope)
	}
}

func TestLinearForecastFlatAndDegenerate(t *testing.T) {
	if slope, _ := LinearForecast([]float64{3, 3, 3, 3}); slope != 0 {
		t.Errorf("flat slope = %v, want 0", slope)
	}
	if slope, intercept := LinearForecast(nil); slope != 0 || intercept != 0 {
		t.Errorf("empty = (%v,%v), want (0,0)", slope, intercept)
	}
	if slope, intercept := LinearForecast([]float64{7}); slope != 0 || intercept != 7 {
		t.Errorf("single = (%v,%v), want (0,7)", slope, intercept)
	}
}

func TestTimeToThresholdRisingBreach(t *testing.T) {
	// Rising toward a ceiling: current 5, +1/window, cap 10 → 5 windows, finite.
	steps, breach := TimeToThreshold(5, 1, 10)
	if !breach {
		t.Fatal("rising series toward ceiling should breach")
	}
	if math.IsInf(steps, 0) || math.Abs(steps-5) > 1e-9 {
		t.Errorf("time-to-threshold = %v, want finite 5", steps)
	}
}

func TestTimeToThresholdFallingBreach(t *testing.T) {
	// Falling toward a floor: current 100, -10/window, floor 0 → 10 windows.
	steps, breach := TimeToThreshold(100, -10, 0)
	if !breach {
		t.Fatal("falling series toward floor should breach")
	}
	if math.Abs(steps-10) > 1e-9 {
		t.Errorf("time-to-floor = %v, want 10", steps)
	}
}

func TestTimeToThresholdNoBreach(t *testing.T) {
	// Flat series never breaches.
	if steps, breach := TimeToThreshold(5, 0, 10); breach || !math.IsInf(steps, 1) {
		t.Errorf("flat: got (%v, breach=%v), want (+Inf, false)", steps, breach)
	}
	// Slope points away from the threshold → no breach.
	if _, breach := TimeToThreshold(5, -1, 10); breach {
		t.Error("slope away from ceiling should not breach")
	}
	// Already at the threshold → breach now (0 steps).
	if steps, breach := TimeToThreshold(10, 1, 10); !breach || steps != 0 {
		t.Errorf("at threshold: got (%v, breach=%v), want (0, true)", steps, breach)
	}
}

func TestProject(t *testing.T) {
	// Fit over 5 points of y=x+1 (last index 4), project 3 ahead → x=7 → 8.
	if got := Project(5, 1, 1, 3); math.Abs(got-8) > 1e-9 {
		t.Errorf("Project = %v, want 8", got)
	}
}

func TestEWMASmoke(t *testing.T) {
	// EWMA must lie within the data range and, with a high alpha, sit near the
	// most recent value.
	pts := []float64{1, 2, 3, 4}
	got := EWMA(pts, 0.5)
	if got < 1 || got > 4 {
		t.Errorf("EWMA = %v, want within [1,4]", got)
	}
	if hi := EWMA(pts, 1.0); math.Abs(hi-4) > 1e-9 {
		t.Errorf("EWMA alpha=1 = %v, want last value 4", hi)
	}
	if EWMA(nil, 0.5) != 0 {
		t.Error("EWMA of empty series should be 0")
	}
	// Out-of-range alpha is clamped, not panicking.
	if got := EWMA(pts, 5); got < 1 || got > 4 {
		t.Errorf("EWMA clamped-alpha = %v, want within [1,4]", got)
	}
}

func TestAfterReset(t *testing.T) {
	pts := []float64{10, 11, 99, 100, 101}
	// Reset at the deploy (index 2) drops the pre-deploy level.
	sub := AfterReset(pts, 2)
	if len(sub) != 3 || sub[0] != 99 {
		t.Errorf("AfterReset(2) = %v, want [99 100 101]", sub)
	}
	// Deploy-aware trend: post-reset slope is small/positive, not dominated by
	// the jump at the deploy boundary.
	if slope, _ := LinearForecast(AfterReset(pts, 2)); slope <= 0 {
		t.Errorf("post-reset slope = %v, want > 0", slope)
	}
	// Out-of-range / non-positive reset returns the full series.
	if got := AfterReset(pts, -1); len(got) != len(pts) {
		t.Errorf("AfterReset(-1) len = %d, want %d", len(got), len(pts))
	}
	if got := AfterReset(pts, 99); len(got) != len(pts) {
		t.Errorf("AfterReset(oob) len = %d, want %d", len(got), len(pts))
	}
}

func TestSeasonalBaseline(t *testing.T) {
	// Period-3 seasonal signal: same phase as the next slot (index 6) is
	// indices 3 and 0 → values 10 and 10 → baseline 10.
	pts := []float64{10, 20, 30, 10, 20, 30}
	base, ok := SeasonalBaseline(pts, 3, -1)
	if !ok {
		t.Fatal("expected a baseline with >=1 full period")
	}
	if math.Abs(base-10) > 1e-9 {
		t.Errorf("seasonal baseline = %v, want 10", base)
	}
	// Insufficient history → withhold.
	if _, ok := SeasonalBaseline([]float64{1, 2}, 5, -1); ok {
		t.Error("expected withhold when <1 full period available")
	}
	// Non-positive period is rejected.
	if _, ok := SeasonalBaseline(pts, 0, -1); ok {
		t.Error("period <= 0 must not produce a baseline")
	}
	// Reset past all same-phase history → withhold.
	if _, ok := SeasonalBaseline(pts, 3, 5); ok {
		t.Error("reset past same-phase history must withhold")
	}
}

func TestDeviation(t *testing.T) {
	// 20% above a baseline of 100.
	if d := Deviation(120, 100); math.Abs(d-0.2) > 1e-9 {
		t.Errorf("Deviation = %v, want 0.2", d)
	}
	// Below baseline is negative.
	if d := Deviation(80, 100); d >= 0 {
		t.Errorf("below-baseline deviation = %v, want < 0", d)
	}
	// Unit floor: a spike out of a flat-zero baseline is a large deviation, not
	// a divide-by-zero.
	if d := Deviation(50, 0); math.Abs(d-50) > 1e-9 {
		t.Errorf("flat-zero deviation = %v, want 50", d)
	}
}
