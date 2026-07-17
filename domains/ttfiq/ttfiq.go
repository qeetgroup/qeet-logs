// Package ttfiq computes a best-effort "Time To First Independent Query"
// analytic (PRD 7.5): the elapsed time between a tenant's onboarding (earliest
// API-key creation) and their first self-service query (earliest query audit
// event), plus a median across onboarding cohorts when more than one API key
// exists.
//
// Assumptions (best-effort — the caller owns the source queries):
//   - Onboarding time == earliest api_keys.created_at for the tenant.
//   - A "query event" == an audit_log row the caller classifies as a query
//     (in the handler: action in {query, export, auth-events}); the caller
//     passes those timestamps in.
//   - Each API key is treated as an onboarding "cohort"; a cohort's TTFIQ is the
//     gap from that key's creation to the first query event at/after it.
//   - The overall TTFIQ uses the earliest key and the earliest query event; a
//     query event that predates onboarding is treated as a data anomaly and the
//     overall TTFIQ is left unset (nil) rather than reported as negative.
package ttfiq

import (
	"sort"
	"time"
)

// Summary is the computed TTFIQ result. Pointer fields are nil when the
// underlying value is not derivable from the available data.
type Summary struct {
	OnboardedAt        *time.Time
	FirstQueryAt       *time.Time
	TTFIQSeconds       *float64
	CohortCount        int
	MedianTTFIQSeconds *float64
}

// Compute derives the TTFIQ summary from a tenant's API-key creation times
// (onboarding cohorts) and the timestamps of its query audit events. Neither
// slice needs to be pre-sorted; Compute sorts copies and does not mutate input.
func Compute(keyTimes, queryTimes []time.Time) Summary {
	ks := sortedCopy(keyTimes)
	qs := sortedCopy(queryTimes)

	s := Summary{CohortCount: len(ks)}
	if len(ks) > 0 {
		t := ks[0]
		s.OnboardedAt = &t
	}
	if len(qs) > 0 {
		t := qs[0]
		s.FirstQueryAt = &t
	}
	if s.OnboardedAt != nil && s.FirstQueryAt != nil {
		if d := s.FirstQueryAt.Sub(*s.OnboardedAt).Seconds(); d >= 0 {
			s.TTFIQSeconds = &d
		}
	}

	// A median across cohorts is only meaningful with more than one cohort.
	if len(ks) > 1 {
		var perCohort []float64
		for _, k := range ks {
			if q, ok := firstAtOrAfter(qs, k); ok {
				perCohort = append(perCohort, q.Sub(k).Seconds())
			}
		}
		if m, ok := median(perCohort); ok {
			s.MedianTTFIQSeconds = &m
		}
	}
	return s
}

func sortedCopy(in []time.Time) []time.Time {
	out := make([]time.Time, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// firstAtOrAfter returns the earliest time in the (ascending) slice that is not
// before t.
func firstAtOrAfter(sorted []time.Time, t time.Time) (time.Time, bool) {
	i := sort.Search(len(sorted), func(i int) bool { return !sorted[i].Before(t) })
	if i < len(sorted) {
		return sorted[i], true
	}
	return time.Time{}, false
}

func median(vals []float64) (float64, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2], true
	}
	return (s[n/2-1] + s[n/2]) / 2, true
}
