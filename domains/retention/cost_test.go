package retention

import "testing"

func TestEstimateCost(t *testing.T) {
	// 10 GB/day logs, 2 GB/day metrics, retained 30 days at $0.10/GB-month.
	daily := map[string]int64{"logs": 10_000_000_000, "metrics": 2_000_000_000}
	e := EstimateCost(daily, 30, 0.10)

	if e.RetentionDays != 30 || e.RatePerGBMonth != 0.10 {
		t.Fatalf("meta = %+v", e)
	}
	if len(e.Signals) != 2 || e.Signals[0].Signal != "logs" {
		t.Fatalf("signals = %+v", e.Signals)
	}
	// logs: 10GB×30 = 300 GB → $30.00
	if e.Signals[0].RetainedGB != 300 || e.Signals[0].MonthlyUSD != 30 {
		t.Errorf("logs cost = %+v, want 300GB/$30", e.Signals[0])
	}
	// total: 300 + 60 = 360 GB → $36.00
	if e.TotalGB != 360 || e.TotalMonthlyUSD != 36 {
		t.Errorf("total = %vGB/$%v, want 360/$36", e.TotalGB, e.TotalMonthlyUSD)
	}
}

func TestWhatIfRetention(t *testing.T) {
	daily := map[string]int64{"logs": 1_000_000_000} // 1 GB/day
	previews := WhatIfRetention(daily, []int{7, 30, 90}, 0.10)
	if len(previews) != 3 {
		t.Fatalf("want 3 previews, got %d", len(previews))
	}
	// 7d → 7 GB → $0.70 ; 90d → 90 GB → $9.00 (cost scales linearly with window)
	if previews[0].TotalGB != 7 || previews[2].TotalGB != 90 {
		t.Errorf("what-if GB = %v / %v, want 7 / 90", previews[0].TotalGB, previews[2].TotalGB)
	}
	if previews[2].TotalMonthlyUSD <= previews[0].TotalMonthlyUSD {
		t.Error("longer retention must cost more")
	}
}
