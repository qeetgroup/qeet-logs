package rca

import (
	"reflect"
	"testing"
)

// sampleCandidates mirrors what RetrieveForService emits: a recent deploy, a
// high-error/high-volume dependency, and a low-error/low-volume dependency.
func sampleCandidates() []Candidate {
	return []Candidate{
		{Type: "deploy", Subject: "deploy checkout v2", Score: 0.88, Evidence: map[string]any{"git_sha": "abc123"}},
		{Type: "dependency", Subject: "payments", Score: 0.85, Evidence: map[string]any{"calls": int64(1000), "errors": int64(900)}},
		{Type: "dependency", Subject: "cache", Score: 0.42, Evidence: map[string]any{"calls": int64(10), "errors": int64(1)}},
	}
}

func TestRankOrder(t *testing.T) {
	ranked := Rank(sampleCandidates(), DefaultWeights())

	wantSubjects := []string{"deploy checkout v2", "payments", "cache"}
	if len(ranked) != len(wantSubjects) {
		t.Fatalf("got %d candidates, want %d", len(ranked), len(wantSubjects))
	}
	for i, want := range wantSubjects {
		if ranked[i].Subject != want {
			t.Errorf("rank %d: got %q (score %.2f), want %q", i, ranked[i].Subject, ranked[i].Score, want)
		}
	}

	// Scores must be non-increasing and inside [0,1].
	for i := range ranked {
		if ranked[i].Score < 0 || ranked[i].Score > 1 {
			t.Errorf("candidate %d score %.2f out of [0,1]", i, ranked[i].Score)
		}
		if i > 0 && ranked[i-1].Score < ranked[i].Score {
			t.Errorf("scores not sorted descending: [%d]=%.2f < [%d]=%.2f", i-1, ranked[i-1].Score, i, ranked[i].Score)
		}
	}
}

func TestRankIsPure(t *testing.T) {
	in := sampleCandidates()
	before := make([]float64, len(in))
	for i := range in {
		before[i] = in[i].Score
	}

	_ = Rank(in, DefaultWeights())

	for i := range in {
		if in[i].Score != before[i] {
			t.Errorf("Rank mutated input candidate %d score: %.2f -> %.2f", i, before[i], in[i].Score)
		}
	}
}

func TestRankRecencyFromExplicitAge(t *testing.T) {
	// Two deploys with identical retrieval Score but different explicit ages: the
	// more recent one must rank first once recency is recomputed from age.
	cands := []Candidate{
		{Type: "deploy", Subject: "old", Score: 0.5, Evidence: map[string]any{"age_seconds": 7200}},
		{Type: "deploy", Subject: "fresh", Score: 0.5, Evidence: map[string]any{"age_seconds": 60}},
	}
	ranked := Rank(cands, DefaultWeights())
	if ranked[0].Subject != "fresh" {
		t.Fatalf("expected fresher deploy first, got %q then %q", ranked[0].Subject, ranked[1].Subject)
	}
	if ranked[0].Score <= ranked[1].Score {
		t.Errorf("fresh score %.2f should exceed old score %.2f", ranked[0].Score, ranked[1].Score)
	}
}

func TestAboveGate(t *testing.T) {
	ranked := Rank(sampleCandidates(), DefaultWeights())
	const gate = 0.3

	var passed int
	for _, c := range ranked {
		if AboveGate(c, gate) {
			passed++
		}
	}
	// Deploy + high-error dependency clear 0.3; the low-error dependency does not.
	if passed != 2 {
		t.Errorf("expected 2 candidates above gate %.2f, got %d (scores: %v)", gate, passed, scores(ranked))
	}

	if !AboveGate(Candidate{Score: 0.9}, 0.5) {
		t.Error("0.9 should be above gate 0.5")
	}
	if AboveGate(Candidate{Score: 0.4}, 0.5) {
		t.Error("0.4 should be below gate 0.5")
	}
	// Boundary: equal score clears the gate (>=).
	if !AboveGate(Candidate{Score: 0.5}, 0.5) {
		t.Error("0.5 should clear gate 0.5 (inclusive)")
	}
}

func TestDefaultWeights(t *testing.T) {
	w := DefaultWeights()
	if w.Type <= 0 || w.Recency <= 0 || w.ErrorRate <= 0 || w.Blast <= 0 {
		t.Errorf("all default weights must be positive, got %+v", w)
	}
	// Type prior should lead the weighting (deploy > dependency emphasis).
	if w.Type < w.Recency || w.Type < w.ErrorRate || w.Type < w.Blast {
		t.Errorf("Type weight expected to lead, got %+v", w)
	}
}

func TestWeightsFromEnvDefaults(t *testing.T) {
	// With no env vars set, WeightsFromEnv must equal DefaultWeights.
	t.Setenv("RCA_RANK_W_TYPE", "")
	t.Setenv("RCA_RANK_W_RECENCY", "")
	t.Setenv("RCA_RANK_W_ERRORRATE", "")
	t.Setenv("RCA_RANK_W_BLAST", "")
	if got := WeightsFromEnv(); !reflect.DeepEqual(got, DefaultWeights()) {
		t.Errorf("WeightsFromEnv with empty env = %+v, want %+v", got, DefaultWeights())
	}
}

func TestWeightsFromEnvOverride(t *testing.T) {
	t.Setenv("RCA_RANK_W_TYPE", "0.9")
	t.Setenv("RCA_RANK_W_RECENCY", "bogus") // malformed -> keeps default
	t.Setenv("RCA_RANK_W_ERRORRATE", "-1")  // negative -> rejected, keeps default
	w := WeightsFromEnv()
	if w.Type != 0.9 {
		t.Errorf("Type override = %v, want 0.9", w.Type)
	}
	if w.Recency != DefaultWeights().Recency {
		t.Errorf("malformed Recency should keep default %v, got %v", DefaultWeights().Recency, w.Recency)
	}
	if w.ErrorRate != DefaultWeights().ErrorRate {
		t.Errorf("negative ErrorRate should keep default %v, got %v", DefaultWeights().ErrorRate, w.ErrorRate)
	}
}

func scores(cs []Candidate) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = c.Score
	}
	return out
}
