package buscontext

import "testing"

func TestEstimateExposure(t *testing.T) {
	// Three mappings on one service across two plan tiers, each with an SLA.
	// Strictest SLA is 99.99 → error budget 0.01% → low = 17000 * 0.0001 = 1.70.
	rows := []Mapping{
		{Service: "api", Customer: "acme", PlanTier: "enterprise", MonthlyRevenue: 10000, SLATarget: 99.9},
		{Service: "api", Customer: "globex", PlanTier: "pro", MonthlyRevenue: 5000, SLATarget: 99.99},
		{Service: "api", Customer: "initech", PlanTier: "enterprise", MonthlyRevenue: 2000, SLATarget: 99.9},
	}
	exp := EstimateExposure(rows)

	if exp.MappingCount != 3 {
		t.Errorf("MappingCount = %d, want 3", exp.MappingCount)
	}
	if got := exp.AffectedPlanTiers; len(got) != 2 || got[0] != "enterprise" || got[1] != "pro" {
		t.Errorf("AffectedPlanTiers = %v, want [enterprise pro] (deduped + sorted)", got)
	}
	if got := exp.AffectedCustomers; len(got) != 3 || got[0] != "acme" || got[2] != "initech" {
		t.Errorf("AffectedCustomers = %v, want sorted [acme globex initech]", got)
	}
	if exp.RevenueAtRisk.High != 17000 {
		t.Errorf("RevenueAtRisk.High = %v, want 17000 (sum of monthly revenue)", exp.RevenueAtRisk.High)
	}
	if exp.RevenueAtRisk.Low != 1.7 {
		t.Errorf("RevenueAtRisk.Low = %v, want 1.7 (strictest-SLA error-budget share)", exp.RevenueAtRisk.Low)
	}
	if exp.RevenueAtRisk.Low > exp.RevenueAtRisk.High {
		t.Errorf("range invariant broken: low %v > high %v", exp.RevenueAtRisk.Low, exp.RevenueAtRisk.High)
	}
	if exp.RevenueAtRisk.Currency != "USD" || exp.RevenueAtRisk.Basis == "" {
		t.Errorf("revenue range must be qualified: %+v", exp.RevenueAtRisk)
	}
	if exp.WorstSLATarget == nil || *exp.WorstSLATarget != 99.99 {
		t.Errorf("WorstSLATarget = %v, want strictest 99.99", exp.WorstSLATarget)
	}
}

func TestEstimateExposureNoSLA(t *testing.T) {
	// No SLA declared → WorstSLATarget nil, low = default 5% of exposed revenue.
	rows := []Mapping{
		{Service: "web", Customer: "acme", PlanTier: "free", MonthlyRevenue: 0},
		{Service: "web", Customer: "globex", PlanTier: "pro", MonthlyRevenue: 1000},
	}
	exp := EstimateExposure(rows)

	if exp.WorstSLATarget != nil {
		t.Errorf("WorstSLATarget = %v, want nil when no SLA declared", *exp.WorstSLATarget)
	}
	if exp.RevenueAtRisk.High != 1000 {
		t.Errorf("High = %v, want 1000", exp.RevenueAtRisk.High)
	}
	if exp.RevenueAtRisk.Low != 50 {
		t.Errorf("Low = %v, want 50 (5%% default floor)", exp.RevenueAtRisk.Low)
	}
	if got := exp.AffectedPlanTiers; len(got) != 2 || got[0] != "free" || got[1] != "pro" {
		t.Errorf("AffectedPlanTiers = %v, want [free pro]", got)
	}
}

func TestEstimateExposureEmpty(t *testing.T) {
	exp := EstimateExposure(nil)

	if exp.MappingCount != 0 {
		t.Errorf("MappingCount = %d, want 0", exp.MappingCount)
	}
	if exp.RevenueAtRisk.Low != 0 || exp.RevenueAtRisk.High != 0 {
		t.Errorf("empty exposure must be zero range, got %+v", exp.RevenueAtRisk)
	}
	if exp.WorstSLATarget != nil {
		t.Error("empty exposure must have nil WorstSLATarget")
	}
	// Slices must be non-nil so they serialize as [] not null.
	if exp.AffectedServices == nil || exp.AffectedCustomers == nil || exp.AffectedPlanTiers == nil {
		t.Error("affected-* slices must be non-nil empty slices")
	}
}
