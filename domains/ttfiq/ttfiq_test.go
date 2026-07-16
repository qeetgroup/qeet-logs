package ttfiq

import (
	"testing"
	"time"
)

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func at(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }

func TestCompute_Empty(t *testing.T) {
	s := Compute(nil, nil)
	if s.OnboardedAt != nil || s.FirstQueryAt != nil || s.TTFIQSeconds != nil {
		t.Errorf("empty input should yield all-nil summary, got %+v", s)
	}
	if s.CohortCount != 0 {
		t.Errorf("cohort count should be 0")
	}
}

func TestCompute_NoQueryEvents(t *testing.T) {
	s := Compute([]time.Time{at(0)}, nil)
	if s.OnboardedAt == nil {
		t.Fatalf("expected onboardedAt")
	}
	if s.FirstQueryAt != nil || s.TTFIQSeconds != nil {
		t.Errorf("no query events => no first query / ttfiq")
	}
}

func TestCompute_SingleCohort(t *testing.T) {
	// onboarded at hour 0, first query at hour 2 => 7200s, no median (single cohort).
	s := Compute([]time.Time{at(0)}, []time.Time{at(5), at(2)})
	if s.TTFIQSeconds == nil || *s.TTFIQSeconds != 7200 {
		t.Errorf("expected ttfiq 7200s, got %v", s.TTFIQSeconds)
	}
	if s.MedianTTFIQSeconds != nil {
		t.Errorf("single cohort should not produce a median")
	}
	if s.CohortCount != 1 {
		t.Errorf("expected cohort count 1")
	}
}

func TestCompute_MultipleCohortsMedian(t *testing.T) {
	// keys at 0h, 10h, 20h; queries at 1h, 12h, 25h.
	// cohort 0h -> first query >=0h is 1h  => 3600s
	// cohort 10h -> first query >=10h is 12h => 7200s
	// cohort 20h -> first query >=20h is 25h => 18000s
	// median(3600, 7200, 18000) = 7200
	keys := []time.Time{at(0), at(10), at(20)}
	queries := []time.Time{at(1), at(12), at(25)}
	s := Compute(keys, queries)
	if s.CohortCount != 3 {
		t.Fatalf("expected 3 cohorts")
	}
	if s.MedianTTFIQSeconds == nil || *s.MedianTTFIQSeconds != 7200 {
		t.Errorf("expected median 7200s, got %v", s.MedianTTFIQSeconds)
	}
	// overall = earliest key (0h) -> earliest query (1h) = 3600s.
	if s.TTFIQSeconds == nil || *s.TTFIQSeconds != 3600 {
		t.Errorf("expected overall ttfiq 3600s, got %v", s.TTFIQSeconds)
	}
}

func TestCompute_MedianEvenCount(t *testing.T) {
	// two cohorts: 3600 and 7200 => median 5400.
	keys := []time.Time{at(0), at(10)}
	queries := []time.Time{at(1), at(12)}
	s := Compute(keys, queries)
	if s.MedianTTFIQSeconds == nil || *s.MedianTTFIQSeconds != 5400 {
		t.Errorf("expected median 5400s, got %v", s.MedianTTFIQSeconds)
	}
}

func TestCompute_QueryBeforeOnboardingAnomaly(t *testing.T) {
	// only query predates the only key => overall ttfiq omitted, no cohort value.
	s := Compute([]time.Time{at(5)}, []time.Time{at(1)})
	if s.TTFIQSeconds != nil {
		t.Errorf("query before onboarding should leave ttfiq unset, got %v", *s.TTFIQSeconds)
	}
	if s.FirstQueryAt == nil {
		t.Errorf("first query should still be reported")
	}
}
