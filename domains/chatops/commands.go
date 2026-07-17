package chatops

import (
	"encoding/json"
	"strings"
)

// Two-way ChatOps command surface (PRD Module 19.1 / P2-G7). This is the pure,
// I/O-free half of the inbound direction: it parses the text of a Slack/Teams
// slash-command into an intent, and renders slash-command reply payloads. The
// handler (platform/api/handler/chatops_oauth.go) owns the OAuth install, the
// request-signature verification, tenant resolution, and query execution.

// Action is the verb of a parsed slash-command.
type Action string

const (
	// ActionQuery runs a LogQL++ query and returns the top rows.
	ActionQuery Action = "query"
	// ActionIncidents lists the tenant's open incidents.
	ActionIncidents Action = "incidents"
	// ActionRCA returns a deep link into RCA for a service (execution deferred).
	ActionRCA Action = "rca"
	// ActionHelp renders usage.
	ActionHelp Action = "help"
)

// Command is a parsed slash-command intent. Arg carries the remainder after the
// verb (the LogQL++ text for query, the service name for rca).
type Command struct {
	Action Action
	Arg    string
}

// ParseCommand maps the free text of a slash-command to a Command. The leading
// token selects the action; the rest is the argument. Empty or unknown verbs
// resolve to help so the user always gets a useful reply. Pure.
func ParseCommand(text string) Command {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return Command{Action: ActionHelp}
	}
	verb := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
	switch verb {
	case "query", "q", "search":
		return Command{Action: ActionQuery, Arg: arg}
	case "incidents", "incident", "inc":
		return Command{Action: ActionIncidents}
	case "rca":
		return Command{Action: ActionRCA, Arg: arg}
	case "help", "?":
		return Command{Action: ActionHelp}
	default:
		// Bare text with no known verb is treated as a query for convenience.
		if strings.ContainsAny(text, "={}|") || strings.Contains(strings.ToUpper(text), "SELECT") {
			return Command{Action: ActionQuery, Arg: strings.TrimSpace(text)}
		}
		return Command{Action: ActionHelp}
	}
}

// HelpText is the usage string shown for `help` (and unknown commands).
func HelpText() string {
	return "*Qeet Logs* commands:\n" +
		"• `/qeetlogs query <LogQL++>` — run a query, e.g. `query level=error service=checkout`\n" +
		"• `/qeetlogs incidents` — list open incidents\n" +
		"• `/qeetlogs rca <service>` — open root-cause analysis for a service\n" +
		"• `/qeetlogs help` — this message"
}

// slashResponse is a Slack slash-command reply. response_type "ephemeral" shows
// only to the invoking user; "in_channel" posts to everyone.
type slashResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

// SlashReply renders a slash-command reply payload. inChannel=false keeps the
// reply private to the caller (the default for query results). Never fails.
func SlashReply(text string, inChannel bool) []byte {
	rt := "ephemeral"
	if inChannel {
		rt = "in_channel"
	}
	b, _ := json.Marshal(slashResponse{ResponseType: rt, Text: text})
	return b
}
