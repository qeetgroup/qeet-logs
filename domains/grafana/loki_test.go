package grafana

import (
	"strings"
	"testing"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
)

var testOpts = Options{DefaultLimit: 1000, MaxLimit: 5000}

// fixed reference instants (nanoseconds) so relative-time output is deterministic.
const (
	nowNs   = int64(2_000) * 1_000_000_000 // now  = 2000s
	startNs = int64(1_000) * 1_000_000_000 // start = 1000s ago
	endNs   = int64(2_000) * 1_000_000_000 // end  = now
)

func TestTranslateBasicSelector(t *testing.T) {
	tr, err := Translate(Query{
		Selector: `{service="api",level="error"}`,
		StartNs:  startNs, EndNs: endNs, NowNs: nowNs,
	}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	mustContain(t, tr.LogQLPP,
		"SELECT timestamp, service, level, environment, message, trace_id, span_id FROM logs WHERE",
		"service = 'api'",
		"level = 'error'",
		"time >= now() - 1000s",
		"time <= now()",
		"ORDER BY timestamp DESC",
		"LIMIT 1000",
	)
	if tr.LabelSet["service"] != "api" || tr.LabelSet["level"] != "error" {
		t.Errorf("LabelSet = %v, want service=api level=error", tr.LabelSet)
	}
}

// The translated LogQL++ must actually compile through domains/query and pick
// up the forced tenant predicate — the whole point of routing through LogQL++.
func TestTranslateCompilesWithTenant(t *testing.T) {
	tr, err := Translate(Query{
		Selector: `{service="api"}`,
		StartNs:  startNs, EndNs: endNs, NowNs: nowNs,
	}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	c, err := query.Compile(tr.LogQLPP, "tenant-xyz", query.Options{DefaultLimit: 1000, MaxLimit: 5000})
	if err != nil {
		t.Fatalf("query.Compile(%q): %v", tr.LogQLPP, err)
	}
	mustContain(t, c.SQL,
		"FROM logs WHERE tenant_id = 'tenant-xyz'",
		"service = 'api'",
		"timestamp >= now() - INTERVAL 1000 SECOND",
		"ORDER BY timestamp DESC",
	)
}

func TestTranslateForwardDirection(t *testing.T) {
	tr, err := Translate(Query{Selector: `{service="api"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs, Forward: true}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !strings.Contains(tr.LogQLPP, "ORDER BY timestamp ASC") {
		t.Errorf("forward direction should sort ASC, got: %s", tr.LogQLPP)
	}
}

func TestTranslateNotEqual(t *testing.T) {
	tr, err := Translate(Query{Selector: `{level!="debug"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !strings.Contains(tr.LogQLPP, "level != 'debug'") {
		t.Errorf("missing != predicate, got: %s", tr.LogQLPP)
	}
	if _, ok := tr.LabelSet["level"]; ok {
		t.Errorf("!= matcher must not populate the base stream LabelSet: %v", tr.LabelSet)
	}
}

func TestTranslateLimitCapAndDefault(t *testing.T) {
	over, err := Translate(Query{Selector: `{service="api"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs, Limit: 999_999}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !strings.Contains(over.LogQLPP, "LIMIT 5000") {
		t.Errorf("limit should cap at MaxLimit 5000, got: %s", over.LogQLPP)
	}
	def, _ := Translate(Query{Selector: `{service="api"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts)
	if !strings.Contains(def.LogQLPP, "LIMIT 1000") {
		t.Errorf("unset limit should use DefaultLimit 1000, got: %s", def.LogQLPP)
	}
}

func TestTranslateTimeWindowRounding(t *testing.T) {
	// end 500s ago, start 1500.4s ago (fractional → rounds UP to widen window).
	tr, err := Translate(Query{
		Selector: `{service="api"}`,
		StartNs:  nowNs - (1_500*int64(1_000_000_000) + 400_000_000),
		EndNs:    nowNs - 500*int64(1_000_000_000),
		NowNs:    nowNs,
	}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	mustContain(t, tr.LogQLPP, "time >= now() - 1501s", "time <= now() - 500s")
}

func TestTranslateRejectsRegex(t *testing.T) {
	for _, sel := range []string{`{service=~"api.*"}`, `{service!~"api.*"}`} {
		if _, err := Translate(Query{Selector: sel, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts); err == nil {
			t.Errorf("expected regex matcher %q to be rejected", sel)
		}
	}
}

func TestTranslateRejectsUnknownLabel(t *testing.T) {
	if _, err := Translate(Query{Selector: `{nope="x"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts); err == nil {
		t.Errorf("expected unknown label to be rejected")
	}
}

func TestTranslateRejectsPipeline(t *testing.T) {
	if _, err := Translate(Query{Selector: `{service="api"} |= "boom"`, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts); err == nil {
		t.Errorf("expected log-pipeline stage to be rejected")
	}
}

func TestParseSelectorEmptyBraces(t *testing.T) {
	ms, err := ParseSelector(`{}`)
	if err != nil {
		t.Fatalf("ParseSelector({}): %v", err)
	}
	if len(ms) != 0 {
		t.Errorf("expected no matchers, got %v", ms)
	}
}

func TestParseSelectorMalformed(t *testing.T) {
	for _, sel := range []string{`service="api"`, `{service}`, ``} {
		if _, err := ParseSelector(sel); err == nil {
			t.Errorf("expected %q to be rejected", sel)
		}
	}
}

func TestLogqlQuoteEscaping(t *testing.T) {
	// A value with an embedded single quote must round-trip through the LogQL++
	// lexer to its original form after query.Compile.
	tr, err := Translate(Query{Selector: `{service="a'b"}`, StartNs: startNs, EndNs: endNs, NowNs: nowNs}, testOpts)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !strings.Contains(tr.LogQLPP, `service = 'a\'b'`) {
		t.Errorf("quote not escaped in LogQL++: %s", tr.LogQLPP)
	}
	c, err := query.Compile(tr.LogQLPP, "t", query.Options{DefaultLimit: 100, MaxLimit: 1000})
	if err != nil {
		t.Fatalf("compile escaped value: %v", err)
	}
	if !strings.Contains(c.SQL, `service = 'a\'b'`) {
		t.Errorf("escaped value did not survive compile: %s", c.SQL)
	}
}

func TestLabelsAreSelectableColumns(t *testing.T) {
	// Every browsable label must compile as a LogQL++ predicate on `logs`,
	// otherwise /label/<name>/values and matchers on it would break.
	for _, l := range Labels {
		if !IsLabel(l) {
			t.Errorf("IsLabel(%q) = false", l)
		}
		q := "SELECT " + l + " FROM logs WHERE " + l + " = 'x'"
		if _, err := query.Compile(q, "t", query.Options{DefaultLimit: 10, MaxLimit: 10}); err != nil {
			t.Errorf("label %q is not a selectable logs column: %v", l, err)
		}
	}
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("missing %q\n  in: %s", sub, s)
		}
	}
}
