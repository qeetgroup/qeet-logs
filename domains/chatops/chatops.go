// Package chatops formats Qeet Logs notifications for one-way delivery to
// ChatOps destinations via incoming-webhook URLs (PRD Module 19, outbound
// slice). It is intentionally pure: FormatSlack and FormatTeams take an Event
// and return a ready-to-POST JSON body — no I/O, no config, no state — so the
// mapping from an internal event to each provider's wire format is trivially
// unit-testable and reusable by the alerter delivery path.
//
// This is the NON-GATED slice: one-way delivery only, using an incoming-webhook
// URL the tenant pastes in — no Slack/Teams OAuth app, no bot token. Two-way
// slash-commands (Module 19.1 interactive) and OAuth app install (Module 19.3)
// are GATED and deliberately NOT implemented here.
package chatops

import (
	"encoding/json"
	"strings"
)

// Event is the provider-neutral notification the formatters render. It mirrors
// the shape the alerter already produces (title/service/severity/message + a
// deep link), so wiring the alerter delivery path to ChatOps is a straight map.
type Event struct {
	Title    string // short headline, e.g. "High error rate on checkout-api"
	Service  string // originating service name
	Severity string // critical | error | warning | info (case-insensitive)
	Message  string // human-readable detail / body
	URL      string // deep link back into Qeet Logs (optional)
	Kind     string // event kind, e.g. "alert" | "incident" | "test" (optional)
}

// FormatSlack renders ev as a Slack incoming-webhook payload: a fallback `text`
// plus a single colour-coded attachment carrying Block Kit blocks (headline,
// service/severity/kind fields, message, and a "View in Qeet Logs" button when
// a URL is present). The colour bar encodes severity at a glance.
func FormatSlack(ev Event) []byte {
	title := ev.Title
	if title == "" {
		title = "Qeet Logs notification"
	}

	blocks := []slackBlock{
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: "*" + title + "*"}},
		{Type: "section", Fields: []slackText{
			{Type: "mrkdwn", Text: "*Service:*\n" + orNA(ev.Service)},
			{Type: "mrkdwn", Text: "*Severity:*\n" + orNA(ev.Severity)},
			{Type: "mrkdwn", Text: "*Kind:*\n" + orNA(ev.Kind)},
		}},
	}
	if ev.Message != "" {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: ev.Message},
		})
	}
	if ev.URL != "" {
		blocks = append(blocks, slackBlock{
			Type: "actions",
			Elements: []slackButton{{
				Type: "button",
				Text: slackText{Type: "plain_text", Text: "View in Qeet Logs"},
				URL:  ev.URL,
			}},
		})
	}

	p := slackPayload{
		Text: title,
		Attachments: []slackAttachment{{
			Color:  severityColor(ev.Severity),
			Blocks: blocks,
		}},
	}
	b, _ := json.Marshal(p) // value types only — marshal cannot fail
	return b
}

// FormatTeams renders ev as a legacy MessageCard (the O365 connector card that
// Microsoft Teams incoming webhooks accept): themed by severity, with a facts
// table and an OpenUri action when a URL is present.
func FormatTeams(ev Event) []byte {
	title := ev.Title
	if title == "" {
		title = "Qeet Logs notification"
	}

	section := teamsSection{
		ActivityTitle: orNA(ev.Service),
		Markdown:      true,
		Facts: []teamsFact{
			{Name: "Service", Value: orNA(ev.Service)},
			{Name: "Severity", Value: orNA(ev.Severity)},
			{Name: "Kind", Value: orNA(ev.Kind)},
		},
		Text: ev.Message,
	}

	card := teamsCard{
		Type:       "MessageCard",
		Context:    "http://schema.org/extensions",
		ThemeColor: strings.TrimPrefix(severityColor(ev.Severity), "#"),
		Summary:    title,
		Title:      title,
		Sections:   []teamsSection{section},
	}
	if ev.URL != "" {
		card.Actions = []teamsAction{{
			Type: "OpenUri",
			Name: "View in Qeet Logs",
			Targets: []teamsTarget{{
				OS:  "default",
				URI: ev.URL,
			}},
		}}
	}
	b, _ := json.Marshal(card) // value types only — marshal cannot fail
	return b
}

// severityColor maps a (case-insensitive) severity to a hex colour used for the
// Slack attachment bar and the Teams themeColor. Unknown severities are grey.
func severityColor(sev string) string {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical", "fatal", "emergency", "alert":
		return "#D64545"
	case "error", "err":
		return "#E8590C"
	case "warning", "warn":
		return "#F59F0B"
	case "info", "notice", "information":
		return "#2F80ED"
	default:
		return "#8A94A6"
	}
}

func orNA(s string) string {
	if strings.TrimSpace(s) == "" {
		return "n/a"
	}
	return s
}

// --- Slack incoming-webhook wire types ---

type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color  string       `json:"color,omitempty"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string        `json:"type"`
	Text     *slackText    `json:"text,omitempty"`
	Fields   []slackText   `json:"fields,omitempty"`
	Elements []slackButton `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackButton struct {
	Type string    `json:"type"`
	Text slackText `json:"text"`
	URL  string    `json:"url,omitempty"`
}

// --- Teams MessageCard wire types ---

type teamsCard struct {
	Type       string         `json:"@type"`
	Context    string         `json:"@context"`
	ThemeColor string         `json:"themeColor"`
	Summary    string         `json:"summary"`
	Title      string         `json:"title"`
	Sections   []teamsSection `json:"sections"`
	Actions    []teamsAction  `json:"potentialAction,omitempty"`
}

type teamsSection struct {
	ActivityTitle string      `json:"activityTitle,omitempty"`
	Facts         []teamsFact `json:"facts,omitempty"`
	Text          string      `json:"text,omitempty"`
	Markdown      bool        `json:"markdown"`
}

type teamsFact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type teamsAction struct {
	Type    string        `json:"@type"`
	Name    string        `json:"name"`
	Targets []teamsTarget `json:"targets"`
}

type teamsTarget struct {
	OS  string `json:"os"`
	URI string `json:"uri"`
}
