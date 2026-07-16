package warroom

import (
	"strings"
	"testing"
	"time"
)

func TestBuildHandoff(t *testing.T) {
	opened := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	closed := opened.Add(45 * time.Minute)
	s := Session{ID: "s1", IncidentID: "inc1", Commander: "alice", Summary: "DB pool exhausted", OpenedAt: opened}
	roles := []Role{{Role: "commander", Assignee: "alice"}, {Role: "comms", Assignee: "bob"}}
	entries := []Entry{
		{Kind: "note", Author: "alice", Body: "paged in", CreatedAt: opened},
		{Kind: "action", Author: "bob", Body: "bump pool size", CreatedAt: opened.Add(5 * time.Minute)},
		{Kind: "action", Author: "alice", Body: "add alert on saturation", CreatedAt: opened.Add(10 * time.Minute)},
	}

	h := BuildHandoff(s, roles, entries, closed)

	if h.DurationMinutes != 45 {
		t.Errorf("duration = %v, want 45", h.DurationMinutes)
	}
	if h.TimelineCount != 3 {
		t.Errorf("timeline = %d, want 3", h.TimelineCount)
	}
	if len(h.ActionItems) != 2 || h.ActionItems[0] != "bump pool size" {
		t.Errorf("action items = %v, want 2 (bump pool size first)", h.ActionItems)
	}
	if !strings.Contains(h.Narrative, "Commander: alice") || !strings.Contains(h.Narrative, "commander=alice") {
		t.Errorf("narrative missing command structure:\n%s", h.Narrative)
	}
	if !strings.Contains(h.Narrative, "bump pool size") {
		t.Errorf("narrative missing action item:\n%s", h.Narrative)
	}
}

func TestBuildHandoffNegativeDurationClamped(t *testing.T) {
	opened := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	h := BuildHandoff(Session{IncidentID: "x", OpenedAt: opened}, nil, nil, opened.Add(-time.Hour))
	if h.DurationMinutes != 0 {
		t.Errorf("negative duration must clamp to 0, got %v", h.DurationMinutes)
	}
	if len(h.ActionItems) != 0 {
		t.Errorf("no entries → no action items, got %v", h.ActionItems)
	}
}
