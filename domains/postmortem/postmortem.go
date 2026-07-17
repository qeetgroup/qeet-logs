// Package postmortem implements incident postmortems + the remediation
// knowledge graph (PRD Module 20) and the CERT-In 6-hour incident export
// (PRD Module 27.2).
//
// CERT-In (India's Computer Emergency Response Team) directs that reportable
// cyber-security incidents be reported within 6 hours of being noticed. A
// resolved incident's postmortem already carries everything that report needs —
// detection time, affected systems, severity, impact, root cause and the
// actions taken — so BuildCERTInExport reshapes a (postmortem, incident) pair
// into that fixed, documented structure.
//
// BuildCERTInExport is a PURE function: it performs no I/O, reads no clock and
// is fully deterministic given its inputs, which makes it trivially unit
// testable. All persistence + HTTP wiring lives in the query API handler.
package postmortem

import (
	"math"
	"time"
)

// CERTInDeadlineHours is CERT-In's reporting window: a reportable incident must
// be reported within 6 hours of detection.
const CERTInDeadlineHours = 6.0

// Postmortem is the tenant-scoped postmortem record (postmortems table).
type Postmortem struct {
	ID          string
	TenantID    string
	IncidentID  string // "" when the postmortem is not linked to an incident
	Title       string
	Summary     string
	Timeline    string
	RootCause   string
	Impact      string
	Status      string // draft | published
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time // nil until published
}

// IncidentMeta is the subset of the incidents row the CERT-In export needs.
// The zero value is valid and means "no linked incident".
type IncidentMeta struct {
	ID         string
	Service    string
	Severity   string    // low | medium | high | critical
	Status     string    // open | resolved
	FirstSeen  time.Time // detection time — CERT-In "detected_at"
	ResolvedAt *time.Time
}

// CERTInReport is the structured 6-hour incident export handed to CERT-In. The
// field set is fixed by PRD Module 27.2.
type CERTInReport struct {
	// IncidentType classifies the incident from its severity (see classifyType).
	IncidentType string `json:"incident_type"`
	// DetectedAt is when the incident was first observed (incident.first_seen).
	DetectedAt time.Time `json:"detected_at"`
	// ReportedWithinHours is the elapsed hours from detection to the postmortem
	// being reported (published_at, else created_at). CERT-In mandates <= 6h.
	ReportedWithinHours float64 `json:"reported_within_hours"`
	// AffectedSystems lists impacted services (the incident's service, if known).
	AffectedSystems []string `json:"affected_systems"`
	// Severity mirrors the incident severity (low|medium|high|critical).
	Severity string `json:"severity"`
	// Impact is the business/user impact narrative.
	Impact string `json:"impact"`
	// RootCause is the identified root cause.
	RootCause string `json:"root_cause"`
	// ActionsTaken is the incident timeline — the record of what was done.
	ActionsTaken string `json:"actions_taken"`
	// Status is the current disposition: resolved | ongoing | under_investigation.
	Status string `json:"status"`
}

// BuildCERTInExport reshapes a postmortem and its (optional) incident into the
// fixed CERT-In 6-hour report. It is pure and deterministic.
func BuildCERTInExport(pm Postmortem, inc IncidentMeta) CERTInReport {
	return CERTInReport{
		IncidentType:        classifyType(inc.Severity),
		DetectedAt:          inc.FirstSeen,
		ReportedWithinHours: reportedWithinHours(inc.FirstSeen, reportTime(pm)),
		AffectedSystems:     affectedSystems(inc.Service),
		Severity:            defaultStr(inc.Severity, "unknown"),
		Impact:              defaultStr(pm.Impact, pm.Summary),
		RootCause:           pm.RootCause,
		ActionsTaken:        pm.Timeline,
		Status:              reportStatus(inc),
	}
}

// reportTime is when the postmortem was reported: published_at once published,
// otherwise the creation time.
func reportTime(pm Postmortem) time.Time {
	if pm.PublishedAt != nil && !pm.PublishedAt.IsZero() {
		return *pm.PublishedAt
	}
	return pm.CreatedAt
}

// reportedWithinHours is the hours from detection to reporting, rounded to two
// decimals. It returns 0 when detection time is unknown or reporting is not
// after detection.
func reportedWithinHours(detected, reported time.Time) float64 {
	if detected.IsZero() || reported.IsZero() || !reported.After(detected) {
		return 0
	}
	h := reported.Sub(detected).Hours()
	return math.Round(h*100) / 100
}

// classifyType maps incident severity to a CERT-In incident category.
func classifyType(severity string) string {
	switch severity {
	case "critical":
		return "critical_infrastructure_disruption"
	case "high":
		return "service_disruption"
	case "medium":
		return "service_degradation"
	default:
		return "operational_anomaly"
	}
}

// affectedSystems returns the impacted services; an empty (non-nil) slice when
// the affected service is unknown, so the JSON is [] rather than null.
func affectedSystems(service string) []string {
	if service == "" {
		return []string{}
	}
	return []string{service}
}

// reportStatus derives the CERT-In disposition from the incident state.
func reportStatus(inc IncidentMeta) string {
	if inc.ID == "" {
		return "under_investigation"
	}
	if inc.Status == "resolved" || inc.ResolvedAt != nil {
		return "resolved"
	}
	return "ongoing"
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
