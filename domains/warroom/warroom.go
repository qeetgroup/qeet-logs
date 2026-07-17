// Package warroom is the Phase-2 incident war-room core (PRD Module 18, the
// non-collaboration slice). An incident is declared into a session with a command
// structure (roles), a live investigation timeline (entries), and a post-incident
// handoff assembled from that record. The two-way Slack/Teams sync (Module 18
// collaboration / Module 19) is gated on the collaboration app infra and is out
// of scope here — this is the durable substrate + the handoff assembler.
package warroom

import (
	"fmt"
	"strings"
	"time"
)

// Role is an incident command assignment.
type Role struct {
	Role     string `json:"role"`
	Assignee string `json:"assignee"`
}

// Entry is one item on the war-room timeline.
type Entry struct {
	Kind      string    `json:"kind"` // note|action|status_change|role
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Session is a declared incident war room.
type Session struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incident_id"`
	Commander  string    `json:"commander"`
	Summary    string    `json:"summary"`
	OpenedAt   time.Time `json:"opened_at"`
}

// Handoff is the post-incident handoff assembled from the session record.
type Handoff struct {
	IncidentID      string   `json:"incident_id"`
	Commander       string   `json:"commander"`
	DurationMinutes float64  `json:"duration_minutes"`
	Roles           []Role   `json:"roles"`
	ActionItems     []string `json:"action_items"`
	TimelineCount   int      `json:"timeline_count"`
	Summary         string   `json:"summary"`
	Narrative       string   `json:"narrative"`
}

// BuildHandoff assembles a post-incident handoff from the session, its roles, and
// its timeline entries. Pure — closedAt is passed in (no clock). Action items are
// the bodies of `action` entries; the narrative is a compact human summary.
func BuildHandoff(s Session, roles []Role, entries []Entry, closedAt time.Time) Handoff {
	dur := closedAt.Sub(s.OpenedAt).Minutes()
	if dur < 0 {
		dur = 0
	}
	h := Handoff{
		IncidentID:      s.IncidentID,
		Commander:       s.Commander,
		DurationMinutes: round1(dur),
		Roles:           roles,
		TimelineCount:   len(entries),
		Summary:         s.Summary,
	}
	for _, e := range entries {
		if e.Kind == "action" && strings.TrimSpace(e.Body) != "" {
			h.ActionItems = append(h.ActionItems, e.Body)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Incident %s handoff\n", s.IncidentID)
	if s.Commander != "" {
		fmt.Fprintf(&b, "Commander: %s\n", s.Commander)
	}
	fmt.Fprintf(&b, "Duration: %.1f min · %d timeline entries · %d action item(s)\n",
		h.DurationMinutes, h.TimelineCount, len(h.ActionItems))
	if len(roles) > 0 {
		parts := make([]string, 0, len(roles))
		for _, r := range roles {
			parts = append(parts, fmt.Sprintf("%s=%s", r.Role, r.Assignee))
		}
		fmt.Fprintf(&b, "Command: %s\n", strings.Join(parts, ", "))
	}
	if s.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", s.Summary)
	}
	for i, a := range h.ActionItems {
		fmt.Fprintf(&b, "  [ ] %d. %s\n", i+1, a)
	}
	h.Narrative = b.String()
	return h
}

func round1(x float64) float64 {
	return float64(int64(x*10+0.5)) / 10
}
