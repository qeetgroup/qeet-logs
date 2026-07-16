package retention

import "math"

// Cost-transparent retention controls (PRD Module 6.4, Phase 2). Rather than a
// black-box bill, estimate steady-state retained storage + monthly cost per
// signal from OBSERVED daily ingest and the retention window, and preview the
// cost of alternative windows — so a tenant sees exactly what drives the number
// and can trade retention for spend.

const bytesPerGB = 1_000_000_000.0

// SignalCost is the estimated steady-state cost of retaining one signal type.
type SignalCost struct {
	Signal     string  `json:"signal"` // logs|metrics|traces
	DailyBytes int64   `json:"daily_bytes"`
	RetainedGB float64 `json:"retained_gb"`
	MonthlyUSD float64 `json:"monthly_usd"`
}

// CostEstimate is the full breakdown for a retention window.
type CostEstimate struct {
	RetentionDays   int          `json:"retention_days"`
	RatePerGBMonth  float64      `json:"rate_per_gb_month"`
	Signals         []SignalCost `json:"signals"`
	TotalGB         float64      `json:"total_gb"`
	TotalMonthlyUSD float64      `json:"total_monthly_usd"`
}

// WhatIf is the total cost at an alternative retention window.
type WhatIf struct {
	RetentionDays   int     `json:"retention_days"`
	TotalGB         float64 `json:"total_gb"`
	TotalMonthlyUSD float64 `json:"total_monthly_usd"`
}

// signalOrder keeps output deterministic regardless of map iteration order.
var signalOrder = []string{"logs", "metrics", "traces"}

// EstimateCost computes retained GB + monthly USD per signal at a retention
// window. dailyBytes maps a signal to its observed bytes/day; steady-state
// retained bytes = dailyBytes × retentionDays.
func EstimateCost(dailyBytes map[string]int64, retentionDays int, ratePerGBMonth float64) CostEstimate {
	if retentionDays < 0 {
		retentionDays = 0
	}
	est := CostEstimate{RetentionDays: retentionDays, RatePerGBMonth: ratePerGBMonth}
	seen := map[string]bool{}
	add := func(sig string, daily int64) {
		gb := round3(float64(daily) * float64(retentionDays) / bytesPerGB)
		usd := round2(gb * ratePerGBMonth)
		est.Signals = append(est.Signals, SignalCost{Signal: sig, DailyBytes: daily, RetainedGB: gb, MonthlyUSD: usd})
		est.TotalGB += gb
		est.TotalMonthlyUSD += usd
	}
	for _, sig := range signalOrder {
		if daily, ok := dailyBytes[sig]; ok {
			add(sig, daily)
			seen[sig] = true
		}
	}
	// Any signals outside the canonical order (forward-compat) appended after.
	for sig, daily := range dailyBytes {
		if !seen[sig] {
			add(sig, daily)
		}
	}
	est.TotalGB = round3(est.TotalGB)
	est.TotalMonthlyUSD = round2(est.TotalMonthlyUSD)
	return est
}

// WhatIfRetention previews the total cost at each candidate retention window.
func WhatIfRetention(dailyBytes map[string]int64, days []int, ratePerGBMonth float64) []WhatIf {
	out := make([]WhatIf, 0, len(days))
	for _, d := range days {
		e := EstimateCost(dailyBytes, d, ratePerGBMonth)
		out = append(out, WhatIf{RetentionDays: d, TotalGB: e.TotalGB, TotalMonthlyUSD: e.TotalMonthlyUSD})
	}
	return out
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
