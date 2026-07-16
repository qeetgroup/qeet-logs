package billing

import "testing"

// proPlan: 10M events + 50 GB included; $2/1e6 events, $0.50/GB over.
var proPlan = Plan{
	Plan:                    "pro",
	IncludedEvents:          10_000_000,
	IncludedGB:              50,
	OveragePerMillionEvents: 2.0,
	OveragePerGB:            0.50,
}

func TestComputeInvoicePreview_UnderLimit(t *testing.T) {
	// 5M events, 20 GB — both well under the allowance → zero overage/cost.
	got := ComputeInvoicePreview(proPlan, 5_000_000, 20*1_000_000_000)

	if got.OverageEvents != 0 || got.OverageGB != 0 {
		t.Errorf("overage = %d events / %v GB, want 0 / 0", got.OverageEvents, got.OverageGB)
	}
	if got.OverageEventsCost != 0 || got.OverageGBCost != 0 || got.TotalCost != 0 {
		t.Errorf("cost = %+v, want all zero", got)
	}
	if got.IncludedEvents != 10_000_000 || got.IncludedGB != 50 {
		t.Errorf("allowances not echoed: %+v", got)
	}
}

func TestComputeInvoicePreview_AtLimit(t *testing.T) {
	// Exactly at the allowance → still zero overage (boundary is inclusive).
	got := ComputeInvoicePreview(proPlan, 10_000_000, 50*1_000_000_000)
	if got.TotalCost != 0 || got.OverageEvents != 0 || got.OverageGB != 0 {
		t.Errorf("at-limit must be free, got %+v", got)
	}
}

func TestComputeInvoicePreview_OverLimitProration(t *testing.T) {
	// 12.5M events (2.5M over) + 60.5 GB (10.5 GB over).
	// events: 2.5 × $2.00           = $5.00
	// storage: 10.5 × $0.50         = $5.25
	// total                          = $10.25
	got := ComputeInvoicePreview(proPlan, 12_500_000, 60_500_000_000)

	if got.OverageEvents != 2_500_000 {
		t.Errorf("overage events = %d, want 2500000", got.OverageEvents)
	}
	if got.OverageGB != 10.5 {
		t.Errorf("overage GB = %v, want 10.5", got.OverageGB)
	}
	if got.OverageEventsCost != 5.00 {
		t.Errorf("events cost = %v, want 5.00", got.OverageEventsCost)
	}
	if got.OverageGBCost != 5.25 {
		t.Errorf("GB cost = %v, want 5.25", got.OverageGBCost)
	}
	if got.TotalCost != 10.25 {
		t.Errorf("total = %v, want 10.25", got.TotalCost)
	}
}

func TestComputeInvoicePreview_EventsOnlyOverage(t *testing.T) {
	// 1M events over, storage under → only the events dimension bills.
	got := ComputeInvoicePreview(proPlan, 11_000_000, 10*1_000_000_000)
	if got.OverageGBCost != 0 {
		t.Errorf("GB cost = %v, want 0", got.OverageGBCost)
	}
	if got.OverageEventsCost != 2.00 || got.TotalCost != 2.00 {
		t.Errorf("cost = %+v, want $2.00 events / $2.00 total", got)
	}
}

func TestComputeInvoicePreview_FreePlanZeroRates(t *testing.T) {
	// free plan: zero allowances AND zero rates → preview is always $0 even with
	// large usage (free tenants are never charged in preview).
	free := Plan{Plan: "free"}
	got := ComputeInvoicePreview(free, 9_999_999, 999*1_000_000_000)
	if got.TotalCost != 0 {
		t.Errorf("free plan total = %v, want 0", got.TotalCost)
	}
	if got.OverageEvents != 9_999_999 {
		t.Errorf("overage events = %d, want 9999999", got.OverageEvents)
	}
}
