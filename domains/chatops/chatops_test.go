package chatops

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleEvent() Event {
	return Event{
		Title:    "High error rate on checkout-api",
		Service:  "checkout-api",
		Severity: "critical",
		Message:  "error rate 42% over 5m (threshold 5%)",
		URL:      "https://logs.qeet.in/incidents/abc123",
		Kind:     "alert",
	}
}

func TestFormatSlack_ValidJSONWithTitleAndSeverity(t *testing.T) {
	ev := sampleEvent()
	out := FormatSlack(ev)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("FormatSlack produced invalid JSON: %v\npayload: %s", err, out)
	}

	s := string(out)
	if !strings.Contains(s, ev.Title) {
		t.Errorf("Slack payload missing title %q\npayload: %s", ev.Title, s)
	}
	if !strings.Contains(s, ev.Severity) {
		t.Errorf("Slack payload missing severity %q\npayload: %s", ev.Severity, s)
	}
	if !strings.Contains(s, ev.URL) {
		t.Errorf("Slack payload missing deep-link URL %q", ev.URL)
	}
	if _, ok := got["attachments"]; !ok {
		t.Errorf("Slack payload missing attachments key")
	}
}

func TestFormatTeams_ValidJSONWithTitleAndSeverity(t *testing.T) {
	ev := sampleEvent()
	out := FormatTeams(ev)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("FormatTeams produced invalid JSON: %v\npayload: %s", err, out)
	}

	if got["@type"] != "MessageCard" {
		t.Errorf("Teams payload @type = %v, want MessageCard", got["@type"])
	}
	s := string(out)
	if !strings.Contains(s, ev.Title) {
		t.Errorf("Teams payload missing title %q\npayload: %s", ev.Title, s)
	}
	if !strings.Contains(s, ev.Severity) {
		t.Errorf("Teams payload missing severity %q\npayload: %s", ev.Severity, s)
	}
	if !strings.Contains(s, ev.URL) {
		t.Errorf("Teams payload missing deep-link URL %q", ev.URL)
	}
}

// Empty optional fields (message, url, and even title) must still yield valid
// JSON — the /chatops/test endpoint hands us whatever the admin typed.
func TestFormatters_EmptyOptionalFieldsStillValid(t *testing.T) {
	ev := Event{Severity: "warning"} // everything else empty

	for name, out := range map[string][]byte{
		"slack": FormatSlack(ev),
		"teams": FormatTeams(ev),
	} {
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Errorf("%s: invalid JSON for sparse event: %v\npayload: %s", name, err, out)
		}
		if !strings.Contains(string(out), "warning") {
			t.Errorf("%s: severity not rendered for sparse event\npayload: %s", name, out)
		}
	}
}

func TestSeverityColor(t *testing.T) {
	cases := map[string]string{
		"critical": "#D64545",
		"CRITICAL": "#D64545",
		"error":    "#E8590C",
		"warning":  "#F59F0B",
		"info":     "#2F80ED",
		"weird":    "#8A94A6", // unknown → grey
		"":         "#8A94A6",
	}
	for sev, want := range cases {
		if got := severityColor(sev); got != want {
			t.Errorf("severityColor(%q) = %q, want %q", sev, got, want)
		}
	}
}
