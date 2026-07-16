package routingsim

import "testing"

func ruleFor(t *testing.T, id, service, cond string, enabled bool) Rule {
	t.Helper()
	return Rule{
		ID:        id,
		Name:      id,
		Kind:      "threshold",
		Service:   service,
		Condition: cond,
		Enabled:   enabled,
		Channels:  []Channel{{Type: "webhook", Target: "https://hooks.example/" + id}},
	}
}

func findResult(results []Result, id string) (Result, bool) {
	for _, r := range results {
		if r.RuleID == id {
			return r, true
		}
	}
	return Result{}, false
}

func TestSimulate_ServiceScope(t *testing.T) {
	rules := []Rule{
		ruleFor(t, "any", "", "", true),
		ruleFor(t, "api", "api", "", true),
		ruleFor(t, "web", "web", "", true),
	}
	res := Simulate(Event{Service: "api", Severity: "error"}, rules)

	if r, _ := findResult(res, "any"); !r.Matched {
		t.Errorf("rule with empty service scope should match any event")
	}
	if r, _ := findResult(res, "api"); !r.Matched {
		t.Errorf("rule scoped to api should match service=api")
	}
	if r, _ := findResult(res, "web"); r.Matched {
		t.Errorf("rule scoped to web must NOT match service=api")
	}
}

func TestSimulate_ServiceCaseInsensitive(t *testing.T) {
	rules := []Rule{ruleFor(t, "api", "API", "", true)}
	res := Simulate(Event{Service: "api"}, rules)
	if !res[0].Matched {
		t.Errorf("service match should be case-insensitive")
	}
}

func TestSimulate_DisabledRuleNeverMatches(t *testing.T) {
	rules := []Rule{ruleFor(t, "off", "api", "", false)}
	res := Simulate(Event{Service: "api"}, rules)
	if res[0].Matched {
		t.Errorf("disabled rule must not match")
	}
	if len(res[0].Channels) != 0 {
		t.Errorf("disabled rule must not report firing channels")
	}
}

func TestSimulate_SeverityOrdering(t *testing.T) {
	rules := []Rule{ruleFor(t, "warnplus", "", "level >= 'warn'", true)}

	if r := Simulate(Event{Severity: "error"}, rules); !r[0].Matched {
		t.Errorf("error should satisfy level >= warn")
	}
	if r := Simulate(Event{Severity: "info"}, rules); r[0].Matched {
		t.Errorf("info should NOT satisfy level >= warn")
	}
	if r := Simulate(Event{Severity: "warn"}, rules); !r[0].Matched {
		t.Errorf("warn should satisfy level >= warn (inclusive)")
	}
}

func TestSimulate_SeverityEquality(t *testing.T) {
	rules := []Rule{ruleFor(t, "erronly", "", "severity = 'error'", true)}
	if r := Simulate(Event{Severity: "ERROR"}, rules); !r[0].Matched {
		t.Errorf("severity equality should be case-insensitive")
	}
	if r := Simulate(Event{Severity: "warn"}, rules); r[0].Matched {
		t.Errorf("warn should not equal error")
	}
}

func TestSimulate_LabelEquality(t *testing.T) {
	rules := []Rule{ruleFor(t, "prod", "", "labels.env = \"prod\"", true)}

	if r := Simulate(Event{Labels: map[string]string{"env": "prod"}}, rules); !r[0].Matched {
		t.Errorf("label env=prod should match")
	}
	if r := Simulate(Event{Labels: map[string]string{"env": "staging"}}, rules); r[0].Matched {
		t.Errorf("label env=staging should not match env=prod")
	}
	// Missing label => conservative non-match.
	if r := Simulate(Event{Service: "x"}, rules); r[0].Matched {
		t.Errorf("event without the required label must not match")
	}
}

func TestSimulate_MultiClauseAnd(t *testing.T) {
	rules := []Rule{ruleFor(t, "combo", "api", "level >= 'error' AND labels.region = 'eu'", true)}

	ev := Event{Service: "api", Severity: "fatal", Labels: map[string]string{"region": "eu"}}
	if r := Simulate(ev, rules); !r[0].Matched {
		t.Errorf("all clauses satisfied should match; reasons=%v", r[0].Reasons)
	}
	ev2 := Event{Service: "api", Severity: "fatal", Labels: map[string]string{"region": "us"}}
	if r := Simulate(ev2, rules); r[0].Matched {
		t.Errorf("one failing clause should fail the rule")
	}
}

func TestSimulate_MatchedRuleReportsChannels(t *testing.T) {
	rules := []Rule{ruleFor(t, "api", "api", "", true)}
	res := Simulate(Event{Service: "api"}, rules)
	if len(res[0].Channels) != 1 || res[0].Channels[0].Target != "https://hooks.example/api" {
		t.Errorf("matched rule should surface its channels, got %+v", res[0].Channels)
	}
}

func TestSimulate_UnparseableClauseIsConservative(t *testing.T) {
	rules := []Rule{ruleFor(t, "weird", "", "this is not a clause", true)}
	res := Simulate(Event{Service: "api", Severity: "error"}, rules)
	if res[0].Matched {
		t.Errorf("unparseable condition must be treated as non-matching")
	}
}

func TestSplitClauses_TrimsParens(t *testing.T) {
	cls := splitClauses("(level = 'error')")
	if len(cls) != 1 || cls[0].field != "level" || cls[0].op != "=" || cls[0].value != "error" {
		t.Fatalf("unexpected clause parse: %+v", cls)
	}
}
