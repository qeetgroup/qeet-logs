// Package routingsim provides a pure, side-effect-free matcher that decides
// which alert-rule channels/targets WOULD fire for a synthetic log event,
// without querying ClickHouse or delivering anything (PRD 17.4 — Routing Rule
// Simulation).
//
// The matcher intentionally does NOT evaluate threshold/absence counts — those
// require live ClickHouse data. It answers the routing question: "given an
// event with this service / severity / labels, which enabled rules select it,
// and therefore which channels would receive a notification if that rule
// fired?"
//
// Condition grammar (best-effort): an alert rule's free-text condition is
// treated as a conjunction of AND-separated `field op value` clauses. Supported
// operators are = == != =~ (substring, case-insensitive) and, for severity
// fields only, the ordering operators < <= > >=. OR is not modelled; a clause
// the matcher cannot parse is reported and treated as non-matching
// (conservative — an unknown selector never silently fires a channel).
package routingsim

import (
	"fmt"
	"regexp"
	"strings"
)

// severityRank mirrors the query compiler's level ordering
// (['trace','debug','info','warn','error','fatal']) plus common aliases.
var severityRank = map[string]int{
	"trace":    0,
	"debug":    1,
	"info":     2,
	"warn":     3,
	"warning":  3,
	"error":    4,
	"err":      4,
	"fatal":    5,
	"critical": 5,
	"crit":     5,
}

// Event is the synthetic log event to simulate routing for.
type Event struct {
	Service  string
	Severity string
	Labels   map[string]string
}

// Channel is one delivery target on a rule (mirrors alerting.Channel and the
// stored channels JSONB shape).
type Channel struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// Rule is the routing-relevant subset of an alert rule.
type Rule struct {
	ID        string
	Name      string
	Kind      string
	Service   string // "" = applies to any service
	Condition string // optional selector fragment (AND-separated clauses)
	Enabled   bool
	Channels  []Channel
}

// Result is the routing decision for one rule.
type Result struct {
	RuleID   string    `json:"rule_id"`
	RuleName string    `json:"rule_name"`
	Kind     string    `json:"kind"`
	Matched  bool      `json:"matched"`
	Reasons  []string  `json:"reasons"`
	Channels []Channel `json:"channels,omitempty"` // channels that would fire (matched only)
}

// Simulate evaluates every rule against the event and returns one Result per
// rule (matched or not) with human-readable reasons.
func Simulate(ev Event, rules []Rule) []Result {
	out := make([]Result, 0, len(rules))
	for _, r := range rules {
		out = append(out, matchRule(ev, r))
	}
	return out
}

func matchRule(ev Event, r Rule) Result {
	res := Result{RuleID: r.ID, RuleName: r.Name, Kind: r.Kind, Matched: true}

	if !r.Enabled {
		res.Matched = false
		res.Reasons = append(res.Reasons, "rule is disabled")
		return res
	}

	// Service scope.
	switch {
	case strings.TrimSpace(r.Service) == "":
		res.Reasons = append(res.Reasons, "rule applies to all services")
	case strings.EqualFold(strings.TrimSpace(r.Service), strings.TrimSpace(ev.Service)):
		res.Reasons = append(res.Reasons, fmt.Sprintf("service %q matches rule scope", ev.Service))
	default:
		res.Matched = false
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("service %q does not match rule scope %q", ev.Service, r.Service))
		return res
	}

	// Condition clauses (AND-separated). A failed or unparseable clause fails
	// the whole rule (conservative).
	for _, cl := range splitClauses(r.Condition) {
		ok, reason := evalClause(cl, ev)
		res.Reasons = append(res.Reasons, reason)
		if !ok {
			res.Matched = false
			return res
		}
	}

	res.Channels = r.Channels
	return res
}

type clause struct {
	field string
	op    string
	value string
	raw   string
}

var (
	clauseRe = regexp.MustCompile(`^([\w.]+)\s*(>=|<=|!=|=~|==|=|>|<)\s*(.+)$`)
	andRe    = regexp.MustCompile(`(?i)\s+and\s+`)
)

// splitClauses breaks a condition into AND-separated clauses. Surrounding
// parentheses on a clause are trimmed. An empty condition yields no clauses
// (the rule then matches purely on service scope).
func splitClauses(cond string) []clause {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return nil
	}
	parts := andRe.Split(cond, -1)
	out := make([]clause, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), "()"))
		if p == "" {
			continue
		}
		m := clauseRe.FindStringSubmatch(p)
		if m == nil {
			out = append(out, clause{raw: p})
			continue
		}
		out = append(out, clause{
			field: strings.TrimSpace(m[1]),
			op:    m[2],
			value: unquote(m[3]),
			raw:   p,
		})
	}
	return out
}

func evalClause(cl clause, ev Event) (bool, string) {
	if cl.field == "" || cl.op == "" {
		return false, fmt.Sprintf("unparseable condition clause %q", cl.raw)
	}
	actual, isSeverity, ok := resolveField(cl.field, ev)
	if !ok {
		return false, fmt.Sprintf("event has no %q to satisfy clause %q", cl.field, cl.raw)
	}

	switch cl.op {
	case "=", "==":
		if strings.EqualFold(actual, cl.value) {
			return true, fmt.Sprintf("%s equals %q", cl.field, cl.value)
		}
		return false, fmt.Sprintf("%s is %q, not %q", cl.field, actual, cl.value)
	case "!=":
		if !strings.EqualFold(actual, cl.value) {
			return true, fmt.Sprintf("%s is %q (not %q, satisfied)", cl.field, actual, cl.value)
		}
		return false, fmt.Sprintf("%s equals %q (excluded)", cl.field, cl.value)
	case "=~":
		if strings.Contains(strings.ToLower(actual), strings.ToLower(cl.value)) {
			return true, fmt.Sprintf("%s %q contains %q", cl.field, actual, cl.value)
		}
		return false, fmt.Sprintf("%s %q does not contain %q", cl.field, actual, cl.value)
	case ">", "<", ">=", "<=":
		return evalOrdering(cl, actual, isSeverity)
	}
	return false, fmt.Sprintf("unsupported operator %q in clause %q", cl.op, cl.raw)
}

func evalOrdering(cl clause, actual string, isSeverity bool) (bool, string) {
	if !isSeverity {
		return false, fmt.Sprintf("ordering operator %q only supported on severity/level (clause %q)", cl.op, cl.raw)
	}
	a, okA := severityRank[strings.ToLower(actual)]
	b, okB := severityRank[strings.ToLower(cl.value)]
	if !okA || !okB {
		return false, fmt.Sprintf("unknown severity in clause %q", cl.raw)
	}
	var ok bool
	switch cl.op {
	case ">":
		ok = a > b
	case "<":
		ok = a < b
	case ">=":
		ok = a >= b
	case "<=":
		ok = a <= b
	}
	if ok {
		return true, fmt.Sprintf("severity %q %s %q", actual, cl.op, cl.value)
	}
	return false, fmt.Sprintf("severity %q not %s %q", actual, cl.op, cl.value)
}

// resolveField maps a clause field to a value on the event. service/level/
// severity resolve to the corresponding fields; anything else is treated as a
// label key (with an optional "labels." / "label." prefix). ok is false when
// the event does not carry the field, so the clause is treated as unmatched.
func resolveField(field string, ev Event) (value string, isSeverity, ok bool) {
	switch strings.ToLower(field) {
	case "service":
		return ev.Service, false, ev.Service != ""
	case "level", "severity":
		return ev.Severity, true, ev.Severity != ""
	}
	key := field
	lower := strings.ToLower(field)
	for _, p := range []string{"labels.", "label."} {
		if strings.HasPrefix(lower, p) {
			key = field[len(p):]
			break
		}
	}
	v, present := ev.Labels[key]
	return v, false, present
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
