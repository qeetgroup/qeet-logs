package postmortem

import (
	"testing"
	"time"
)

var detected = time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)

// A fully-linked, resolved, published postmortem produces a complete report.
func TestBuildCERTInExport_ResolvedIncident(t *testing.T) {
	published := detected.Add(3 * time.Hour)
	resolved := detected.Add(90 * time.Minute)
	pm := Postmortem{
		ID:          "pm1",
		TenantID:    "t1",
		IncidentID:  "inc1",
		Title:       "Payments API 5xx surge",
		Summary:     "Elevated 5xx on payments-api",
		Timeline:    "10:00 detected; 10:30 mitigated; 11:30 resolved",
		RootCause:   "Bad config rollout",
		Impact:      "12% of checkout requests failed for 90m",
		Status:      "published",
		CreatedAt:   detected.Add(4 * time.Hour),
		PublishedAt: &published,
	}
	inc := IncidentMeta{
		ID:         "inc1",
		Service:    "payments-api",
		Severity:   "high",
		Status:     "resolved",
		FirstSeen:  detected,
		ResolvedAt: &resolved,
	}

	r := BuildCERTInExport(pm, inc)

	if r.IncidentType != "service_disruption" {
		t.Errorf("incident_type = %q, want service_disruption", r.IncidentType)
	}
	if !r.DetectedAt.Equal(detected) {
		t.Errorf("detected_at = %v, want %v", r.DetectedAt, detected)
	}
	// published_at (not created_at) drives the elapsed window: 3h.
	if r.ReportedWithinHours != 3 {
		t.Errorf("reported_within_hours = %v, want 3", r.ReportedWithinHours)
	}
	if r.ReportedWithinHours > CERTInDeadlineHours {
		t.Errorf("reported_within_hours %v exceeds CERT-In deadline %v", r.ReportedWithinHours, CERTInDeadlineHours)
	}
	if len(r.AffectedSystems) != 1 || r.AffectedSystems[0] != "payments-api" {
		t.Errorf("affected_systems = %v, want [payments-api]", r.AffectedSystems)
	}
	if r.Severity != "high" {
		t.Errorf("severity = %q, want high", r.Severity)
	}
	if r.Impact != pm.Impact {
		t.Errorf("impact = %q, want %q", r.Impact, pm.Impact)
	}
	if r.RootCause != pm.RootCause {
		t.Errorf("root_cause = %q, want %q", r.RootCause, pm.RootCause)
	}
	if r.ActionsTaken != pm.Timeline {
		t.Errorf("actions_taken = %q, want %q", r.ActionsTaken, pm.Timeline)
	}
	if r.Status != "resolved" {
		t.Errorf("status = %q, want resolved", r.Status)
	}
}

// With no linked incident the export still returns a valid, sparse report.
func TestBuildCERTInExport_NoIncident(t *testing.T) {
	pm := Postmortem{
		ID:        "pm2",
		TenantID:  "t1",
		Title:     "Manual writeup",
		Impact:    "Unknown",
		CreatedAt: detected,
	}

	r := BuildCERTInExport(pm, IncidentMeta{})

	if r.IncidentType != "operational_anomaly" {
		t.Errorf("incident_type = %q, want operational_anomaly", r.IncidentType)
	}
	if !r.DetectedAt.IsZero() {
		t.Errorf("detected_at = %v, want zero", r.DetectedAt)
	}
	if r.ReportedWithinHours != 0 {
		t.Errorf("reported_within_hours = %v, want 0", r.ReportedWithinHours)
	}
	if r.AffectedSystems == nil || len(r.AffectedSystems) != 0 {
		t.Errorf("affected_systems = %v, want non-nil empty slice", r.AffectedSystems)
	}
	if r.Severity != "unknown" {
		t.Errorf("severity = %q, want unknown", r.Severity)
	}
	if r.Status != "under_investigation" {
		t.Errorf("status = %q, want under_investigation", r.Status)
	}
}

// Impact falls back to the summary when the dedicated impact field is empty.
func TestBuildCERTInExport_ImpactFallsBackToSummary(t *testing.T) {
	pm := Postmortem{Summary: "brief summary", CreatedAt: detected}
	r := BuildCERTInExport(pm, IncidentMeta{ID: "inc", FirstSeen: detected})
	if r.Impact != "brief summary" {
		t.Errorf("impact = %q, want fallback to summary", r.Impact)
	}
}

// A draft (unpublished) postmortem uses created_at as the reporting time.
func TestBuildCERTInExport_DraftUsesCreatedAt(t *testing.T) {
	pm := Postmortem{Status: "draft", CreatedAt: detected.Add(2 * time.Hour)}
	inc := IncidentMeta{ID: "inc", Severity: "medium", FirstSeen: detected}
	r := BuildCERTInExport(pm, inc)
	if r.ReportedWithinHours != 2 {
		t.Errorf("reported_within_hours = %v, want 2", r.ReportedWithinHours)
	}
	if r.IncidentType != "service_degradation" {
		t.Errorf("incident_type = %q, want service_degradation", r.IncidentType)
	}
}

func TestClassifyTypeViaExport(t *testing.T) {
	cases := map[string]string{
		"critical": "critical_infrastructure_disruption",
		"high":     "service_disruption",
		"medium":   "service_degradation",
		"low":      "operational_anomaly",
		"":         "operational_anomaly",
	}
	for sev, want := range cases {
		r := BuildCERTInExport(Postmortem{CreatedAt: detected}, IncidentMeta{ID: "x", Severity: sev, FirstSeen: detected})
		if r.IncidentType != want {
			t.Errorf("severity %q -> incident_type %q, want %q", sev, r.IncidentType, want)
		}
	}
}

func TestReportedWithinHours(t *testing.T) {
	cases := []struct {
		name             string
		detected         time.Time
		reported         time.Time
		want             float64
		wantWithinWindow bool
	}{
		{"within window", detected, detected.Add(90 * time.Minute), 1.5, true},
		{"rounds to two dp", detected, detected.Add(100 * time.Minute), 1.67, true},
		{"breaches 6h window", detected, detected.Add(7 * time.Hour), 7, false},
		{"reported before detected", detected, detected.Add(-time.Hour), 0, true},
		{"zero detected", time.Time{}, detected, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reportedWithinHours(c.detected, c.reported)
			if got != c.want {
				t.Errorf("reportedWithinHours = %v, want %v", got, c.want)
			}
			if within := got <= CERTInDeadlineHours; within != c.wantWithinWindow {
				t.Errorf("within-window = %v, want %v", within, c.wantWithinWindow)
			}
		})
	}
}
