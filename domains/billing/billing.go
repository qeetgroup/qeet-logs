// Package billing computes usage-vs-plan overage and a per-tenant invoice
// PREVIEW (PRD Module 33.4 / P2-G17). It is deliberately PURE — no I/O, no
// clock, no DB — so the money math is unit-testable and deterministic.
//
// Scope boundary: this package (and its Module 33.4 handlers) stop at PREVIEW.
// Turning a preview into an actual invoice / charge is Module 33.5, delegated to
// Qeet Pay, which is an external gated integration and is NOT implemented here.
package billing

import "math"

// bytesPerGB uses the decimal GB convention (1e9 bytes) to match the retention
// cost model, so "GB" means the same thing across the product.
const bytesPerGB = 1_000_000_000.0

// eventsPerMillion is the divisor for the per-million-events overage rate.
const eventsPerMillion = 1_000_000.0

// Plan is a tenant's billing plan + quota allowances, mirroring the tenant_plans
// row (migration 0017). Rates are USD.
type Plan struct {
	Plan                    string  `json:"plan"`                       // free|pro|enterprise
	IncludedEvents          int64   `json:"included_events"`            // events included per period
	IncludedGB              float64 `json:"included_gb"`                // stored GB included per period
	OveragePerMillionEvents float64 `json:"overage_per_million_events"` // USD per 1e6 events over allowance
	OveragePerGB            float64 `json:"overage_per_gb"`             // USD per GB over allowance
}

// InvoicePreview is the computed, non-binding preview of a tenant's overage for
// a billing period. It is a preview only — see the package doc on Module 33.5.
type InvoicePreview struct {
	IncludedEvents    int64   `json:"included_events"`
	IncludedGB        float64 `json:"included_gb"`
	OverageEvents     int64   `json:"overage_events"`
	OverageGB         float64 `json:"overage_gb"`
	OverageEventsCost float64 `json:"overage_events_cost"`
	OverageGBCost     float64 `json:"overage_gb_cost"`
	TotalCost         float64 `json:"total_cost"`
}

// ComputeInvoicePreview derives overage and cost from a plan and the period's
// measured usage (events + stored bytes). Usage at or under the allowance yields
// zero overage; usage over it is prorated linearly — fractional millions of
// events and fractional GB are billed pro rata against the plan's unit rates.
func ComputeInvoicePreview(plan Plan, usageEvents int64, usageBytes int64) InvoicePreview {
	usageGB := float64(usageBytes) / bytesPerGB

	overageEvents := usageEvents - plan.IncludedEvents
	if overageEvents < 0 {
		overageEvents = 0
	}
	overageGB := usageGB - plan.IncludedGB
	if overageGB < 0 {
		overageGB = 0
	}

	// Linear proration: cost scales with the fractional units over the allowance.
	overageEventsCost := round2((float64(overageEvents) / eventsPerMillion) * plan.OveragePerMillionEvents)
	overageGBCost := round2(overageGB * plan.OveragePerGB)

	return InvoicePreview{
		IncludedEvents:    plan.IncludedEvents,
		IncludedGB:        plan.IncludedGB,
		OverageEvents:     overageEvents,
		OverageGB:         round3(overageGB),
		OverageEventsCost: overageEventsCost,
		OverageGBCost:     overageGBCost,
		TotalCost:         round2(overageEventsCost + overageGBCost),
	}
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
