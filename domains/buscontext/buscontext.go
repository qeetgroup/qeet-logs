// Package buscontext is the Phase-2 Business Context Correlation Layer (PRD
// Module 16, gap P2-G4). It turns a tenant's per-service business mappings —
// which customers / plan-tiers ride a service, the recurring revenue on the
// line, and the SLA promised — into an incident-facing IMPACT estimate: the
// affected plan tiers, a QUALIFIED revenue-at-risk range (low/high, never a
// single bogus point number), and the strictest SLA target exposed.
//
// EstimateExposure is a PURE function over the loaded mappings (no I/O), so the
// business-impact math is unit-testable and deterministic; the load/CRUD helpers
// are thin pgx wrappers that scope every query by tenant_id (the incidents-table
// explicit-tenant-filter convention — no RLS policy on this table).
package buscontext

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultBudgetFraction is the conservative low-bound share of exposed monthly
// revenue used when NO SLA is declared and we therefore have no error budget to
// derive a floor from. 5% ≈ "a partial, time-boxed degradation" rather than a
// sustained full-month outage.
const defaultBudgetFraction = 0.05

// defaultCurrency labels the revenue-at-risk range. Revenue is stored as a bare
// NUMERIC; the correlation layer assumes a single reporting currency per tenant.
const defaultCurrency = "USD"

// Row is a stored business_context record: a (service → customer/plan/revenue/SLA)
// mapping plus its bookkeeping columns.
type Row struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Service        string    `json:"service"`
	Customer       string    `json:"customer"`
	PlanTier       string    `json:"plan_tier"`
	MonthlyRevenue float64   `json:"monthly_revenue"`
	SLATarget      float64   `json:"sla_target"` // 0 = not declared
	Owner          string    `json:"owner"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Input is the admin-supplied payload for creating a mapping.
type Input struct {
	Service        string
	Customer       string
	PlanTier       string
	MonthlyRevenue float64
	SLATarget      float64 // <= 0 is stored as NULL (undeclared)
	Owner          string
	Notes          string
}

// Mapping is the impact-relevant subset of a Row that EstimateExposure consumes.
// Kept distinct from Row so the pure estimator has no dependency on DB bookkeeping.
type Mapping struct {
	ID             string
	Service        string
	Customer       string
	PlanTier       string
	MonthlyRevenue float64
	SLATarget      float64
}

// RevenueRange is a QUALIFIED revenue-at-risk band, never a single point value.
// Basis spells out exactly what Low and High mean so a reader never mistakes the
// range for a precise loss figure.
type RevenueRange struct {
	Low      float64 `json:"low"`
	High     float64 `json:"high"`
	Currency string  `json:"currency"`
	Basis    string  `json:"basis"`
}

// Exposure is the business-impact estimate for a set of affected mappings.
type Exposure struct {
	AffectedServices  []string     `json:"affected_services"`
	AffectedCustomers []string     `json:"affected_customers"`
	AffectedPlanTiers []string     `json:"affected_plan_tiers"`
	RevenueAtRisk     RevenueRange `json:"revenue_at_risk"`
	WorstSLATarget    *float64     `json:"worst_sla_target"` // strictest promised SLA, nil if none declared
	MappingCount      int          `json:"mapping_count"`
}

// EstimateExposure derives the business impact of an incident from the mappings
// on its affected service(s). It is PURE — no I/O, deterministic, order-independent.
//
// Revenue-at-risk is a qualified range, not a point estimate:
//   - High = the full recurring monthly revenue of every affected customer —
//     the worst case of a sustained, full-month, total outage across all of them.
//   - Low  = the revenue inside the STRICTEST SLA's error budget — the slice a
//     single in-budget blip puts at risk — or defaultBudgetFraction of High when
//     no SLA is declared and there is no budget to derive.
//
// WorstSLATarget is the strictest (highest) availability promise among affected
// mappings: the hardest to keep and therefore the one most exposed by an incident.
func EstimateExposure(rows []Mapping) Exposure {
	services := map[string]struct{}{}
	customers := map[string]struct{}{}
	tiers := map[string]struct{}{}
	var totalRevenue, worstSLA float64
	haveSLA := false

	for _, m := range rows {
		if m.Service != "" {
			services[m.Service] = struct{}{}
		}
		if m.Customer != "" {
			customers[m.Customer] = struct{}{}
		}
		if m.PlanTier != "" {
			tiers[m.PlanTier] = struct{}{}
		}
		if m.MonthlyRevenue > 0 {
			totalRevenue += m.MonthlyRevenue
		}
		if m.SLATarget > 0 {
			haveSLA = true
			if m.SLATarget > worstSLA {
				worstSLA = m.SLATarget
			}
		}
	}

	budget := defaultBudgetFraction
	basis := "monthly revenue of affected customers; low = 5% partial-impact floor (no SLA declared), high = full sustained-outage exposure"
	if haveSLA {
		budget = (100 - worstSLA) / 100
		if budget < 0 {
			budget = 0
		}
		basis = fmt.Sprintf("monthly revenue of affected customers; low = strictest-SLA (%.3f%%) error-budget share, high = full sustained-outage exposure", worstSLA)
	}

	exp := Exposure{
		AffectedServices:  sortedKeys(services),
		AffectedCustomers: sortedKeys(customers),
		AffectedPlanTiers: sortedKeys(tiers),
		RevenueAtRisk: RevenueRange{
			Low:      round2(totalRevenue * budget),
			High:     round2(totalRevenue),
			Currency: defaultCurrency,
			Basis:    basis,
		},
		MappingCount: len(rows),
	}
	if haveSLA {
		v := worstSLA
		exp.WorstSLATarget = &v
	}
	return exp
}

// ToMappings projects DB rows down to the impact-relevant Mapping subset.
func ToMappings(rows []Row) []Mapping {
	out := make([]Mapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, Mapping{
			ID:             r.ID,
			Service:        r.Service,
			Customer:       r.Customer,
			PlanTier:       r.PlanTier,
			MonthlyRevenue: r.MonthlyRevenue,
			SLATarget:      r.SLATarget,
		})
	}
	return out
}

// selectCols COALESCEs nullable columns and casts NUMERIC to float8 so rows scan
// cleanly into Row's plain string/float64 fields.
const selectCols = `SELECT id::text, tenant_id::text, service,
       COALESCE(customer, ''), COALESCE(plan_tier, ''),
       COALESCE(monthly_revenue, 0)::float8, COALESCE(sla_target, 0)::float8,
       COALESCE(owner, ''), COALESCE(notes, ''), created_at, updated_at
FROM business_context`

// LoadByService returns every mapping for a tenant's service, highest-revenue first.
func LoadByService(ctx context.Context, pool *pgxpool.Pool, tenant, service string) ([]Row, error) {
	return queryRows(ctx, pool,
		"WHERE tenant_id = $1::uuid AND service = $2 ORDER BY monthly_revenue DESC, customer",
		tenant, service)
}

// LoadAll returns every mapping for a tenant (admin listing).
func LoadAll(ctx context.Context, pool *pgxpool.Pool, tenant string) ([]Row, error) {
	return queryRows(ctx, pool,
		"WHERE tenant_id = $1::uuid ORDER BY service, monthly_revenue DESC",
		tenant)
}

// Create inserts a mapping for the tenant and returns the stored row.
func Create(ctx context.Context, pool *pgxpool.Pool, tenant string, in Input) (Row, error) {
	var sla *float64
	if in.SLATarget > 0 {
		v := in.SLATarget
		sla = &v
	}
	var r Row
	err := pool.QueryRow(ctx, `
		INSERT INTO business_context
		    (tenant_id, service, customer, plan_tier, monthly_revenue, sla_target, owner, notes)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, tenant_id::text, service,
		          COALESCE(customer, ''), COALESCE(plan_tier, ''),
		          COALESCE(monthly_revenue, 0)::float8, COALESCE(sla_target, 0)::float8,
		          COALESCE(owner, ''), COALESCE(notes, ''), created_at, updated_at`,
		tenant, in.Service, textOrNil(in.Customer), textOrNil(in.PlanTier),
		in.MonthlyRevenue, sla, textOrNil(in.Owner), textOrNil(in.Notes),
	).Scan(&r.ID, &r.TenantID, &r.Service, &r.Customer, &r.PlanTier,
		&r.MonthlyRevenue, &r.SLATarget, &r.Owner, &r.Notes, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// Delete removes a mapping scoped to the tenant. Reports whether a row was deleted.
func Delete(ctx context.Context, pool *pgxpool.Pool, tenant, id string) (bool, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM business_context WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, tenant)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func queryRows(ctx context.Context, pool *pgxpool.Pool, where string, args ...any) ([]Row, error) {
	rows, err := pool.Query(ctx, selectCols+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Service, &r.Customer, &r.PlanTier,
			&r.MonthlyRevenue, &r.SLATarget, &r.Owner, &r.Notes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
