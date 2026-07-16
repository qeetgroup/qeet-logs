package rca

// ranker.go is the non-gated slice of the Phase-2 RCA ranking layer (PRD Module
// 11.2). It is the honest intermediate between the Phase-1 structural retriever
// (rca.go / Module 11.1) and the eventual confidence-gated LEARNED-TO-RANK model
// (11.2 GA): a transparent, feature-weighted LINEAR ranker over the candidates
// the retriever already produced.
//
// Why a linear ranker and not an ML model: a trained learned-to-rank model needs
// a labelled corpus (which candidate was actually the root cause) that does not
// exist yet. Rather than fake a "trained" model, we ship a re-ranker whose every
// weight is human-readable and whose features are interpretable, and we start
// collecting labels now (see the rca_feedback table + POST /v1/admin/rca/feedback).
// When enough labels accrue, those same features + the collected labels train the
// GA model; this file's Weights become the model's learned weights. Until then
// the trained learned-to-rank model stays DEFERRED, gated on a labelled corpus.
//
// Rank is deliberately PURE (no I/O, no clock): given candidates + weights it
// recomputes each Score as a normalised weighted linear combination of features
// and re-sorts, without mutating its input. That makes it trivially unit-testable
// and keeps ranking POLICY separate from retrieval.

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
)

// Weights are the (currently hand-tuned, later learned) coefficients of the
// linear ranker. Each weights one interpretable feature in [0,1]; the final score
// is the weighted sum normalised by the total weight, so it stays in [0,1]
// regardless of the absolute magnitudes chosen here.
type Weights struct {
	// Type weights the change-type prior. A deploy on the failing service is a
	// stronger structural suspect than a downstream dependency, so the feature is
	// higher for "deploy" than "dependency" (see typeFeature).
	Type float64 `json:"type"`
	// Recency weights how recent the signal is: recent changes are likelier causes.
	Recency float64 `json:"recency"`
	// ErrorRate weights the measured error rate of a dependency candidate
	// (errors/calls) — the correlated-failure signal.
	ErrorRate float64 `json:"error_rate"`
	// Blast weights the blast radius (call volume affected), log-scaled — a
	// high-traffic dependency erroring is worse than a rarely-called one at the
	// same rate.
	Blast float64 `json:"blast"`
}

// DefaultWeights is the shipped hand-tuned weighting. These sum to 1.0 for
// readability, but Rank normalises by the total weight so any non-negative
// weights work. Type leads (structural prior), then recency, then the
// dependency-specific error-rate and blast signals.
func DefaultWeights() Weights {
	return Weights{
		Type:      0.35,
		Recency:   0.30,
		ErrorRate: 0.25,
		Blast:     0.10,
	}
}

// WeightsFromEnv overlays per-field overrides from the environment onto
// DefaultWeights, so the ranker can be tuned without a rebuild while the learned
// model is still deferred. Each var is a non-negative float; a malformed or
// absent value keeps the default. Vars:
//
//	RCA_RANK_W_TYPE  RCA_RANK_W_RECENCY  RCA_RANK_W_ERRORRATE  RCA_RANK_W_BLAST
func WeightsFromEnv() Weights {
	w := DefaultWeights()
	envFloat("RCA_RANK_W_TYPE", &w.Type)
	envFloat("RCA_RANK_W_RECENCY", &w.Recency)
	envFloat("RCA_RANK_W_ERRORRATE", &w.ErrorRate)
	envFloat("RCA_RANK_W_BLAST", &w.Blast)
	return w
}

func envFloat(key string, dst *float64) {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil && v >= 0 {
		*dst = v
	}
}

// recencyHalfLifeSeconds is the reference horizon for the recency feature when a
// candidate carries an explicit numeric age. A change at age 0 scores 1.0; at one
// horizon it decays to ~0.37 (matching the retriever's exp-decay flavour). Kept a
// constant so Rank stays pure (no time.Now).
const recencyHalfLifeSeconds = 3600.0

// blastLog10Cap is the call volume (log10) at which the blast-radius feature
// saturates to 1.0: 10^4 = 10,000 calls in the window.
const blastLog10Cap = 4.0

// Rank recomputes every candidate's Score as a normalised weighted linear
// combination of its interpretable features and returns a NEW slice sorted
// highest-score-first. It is PURE: the input slice and the candidates' Evidence
// maps are never mutated (only the copied Score value changes).
func Rank(candidates []Candidate, weights Weights) []Candidate {
	total := weights.Type + weights.Recency + weights.ErrorRate + weights.Blast
	if total <= 0 {
		total = 1
	}
	out := make([]Candidate, len(candidates))
	copy(out, candidates)
	for i := range out {
		c := out[i]
		weighted := weights.Type*typeFeature(c) +
			weights.Recency*recencyFeature(c) +
			weights.ErrorRate*errorRateFeature(c) +
			weights.Blast*blastFeature(c)
		out[i].Score = round2(clamp01(weighted / total))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// AboveGate reports whether a candidate clears the confidence gate. Today this is
// a static threshold over the linear score; the learned ranker (11.2 GA) will
// eventually own a calibrated, per-tenant gate here instead.
func AboveGate(c Candidate, min float64) bool {
	return c.Score >= min
}

// typeFeature is the change-type prior: deploy > dependency. A deploy on the
// failing service is the strongest structural suspect (deploy intelligence,
// Module 15, is the #1 RCA signal); a downstream dependency is weaker and indirect.
func typeFeature(c Candidate) float64 {
	switch c.Type {
	case "deploy":
		return 1.0
	case "dependency":
		return 0.6
	default:
		return 0.4
	}
}

// recencyFeature scores how recent the signal is, in [0,1]. It prefers an
// explicit numeric age in the evidence ("age"/"age_seconds") and decays it over
// recencyHalfLifeSeconds. The Phase-1 retriever does not emit a numeric age but
// already bakes recency into a deploy candidate's structural Score, so for deploy
// candidates without an explicit age we fall back to that Score as the recency
// proxy. Other candidates without an age contribute no recency signal.
func recencyFeature(c Candidate) float64 {
	if age, ok := evNum(c.Evidence, "age", "age_seconds"); ok {
		if age < 0 {
			age = 0
		}
		return clamp01(math.Exp(-age / recencyHalfLifeSeconds))
	}
	if c.Type == "deploy" {
		return clamp01(c.Score)
	}
	return 0
}

// errorRateFeature is the correlated-failure signal for dependency candidates:
// errors/calls from the evidence, clamped to [0,1]. 0 when the counts are absent.
func errorRateFeature(c Candidate) float64 {
	errs, ok1 := evNum(c.Evidence, "errors")
	calls, ok2 := evNum(c.Evidence, "calls")
	if !ok1 || !ok2 || calls <= 0 {
		return 0
	}
	return clamp01(errs / calls)
}

// blastFeature is the log-scaled blast radius: a high-traffic dependency erroring
// affects more of the system than a rarely-called one. Uses evidence "calls",
// saturating at blastLog10Cap. 0 when the volume is absent.
func blastFeature(c Candidate) float64 {
	calls, ok := evNum(c.Evidence, "calls")
	if !ok || calls <= 0 {
		return 0
	}
	return clamp01(math.Log10(1+calls) / blastLog10Cap)
}

// evNum reads the first present key from an evidence map and coerces common
// numeric encodings (int/int64/float64/json.Number/numeric string) to float64.
func evNum(ev map[string]any, keys ...string) (float64, bool) {
	if ev == nil {
		return 0, false
	}
	for _, k := range keys {
		v, ok := ev[k]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case float64:
			return x, true
		case float32:
			return float64(x), true
		case int:
			return float64(x), true
		case int64:
			return float64(x), true
		case json.Number:
			if n, err := x.Float64(); err == nil {
				return n, true
			}
		case string:
			if n, err := strconv.ParseFloat(x, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }
