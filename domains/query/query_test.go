package query

import (
	"errors"
	"strings"
	"testing"
)

func compile(t *testing.T, q string) *Compiled {
	t.Helper()
	c, err := Compile(q, "T1", Options{DefaultLimit: 100, MaxLimit: 1000})
	if err != nil {
		t.Fatalf("compile %q: %v", q, err)
	}
	return c
}

func mustContain(t *testing.T, sql string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(sql, s) {
			t.Errorf("SQL missing %q\n  got: %s", s, sql)
		}
	}
}

// TAD §10.2 example 1.
func TestSearchFixture(t *testing.T) {
	c := compile(t, `SEARCH "payment failed" FROM logs WHERE tenant = 'ten_abc123' AND time > now() - 1h`)
	if c.Kind != KindSearch {
		t.Fatalf("kind = %v", c.Kind)
	}
	mustContain(t, c.SQL,
		"SELECT id, timestamp, service, level, message, trace_id, span_id, body FROM logs WHERE tenant_id = 'T1' AND",
		"hasTokenCaseInsensitive(message, 'payment')",
		"hasTokenCaseInsensitive(message, 'failed')",
		"timestamp > now() - INTERVAL 3600 SECOND",
		"ORDER BY timestamp DESC LIMIT 100",
	)
	if strings.Contains(c.SQL, "ten_abc123") {
		t.Errorf("user-supplied tenant must be ignored, got: %s", c.SQL)
	}
}

// TAD §10.2 example 2.
func TestSelectAggregateFixture(t *testing.T) {
	c := compile(t, `SELECT service, count(*) AS errors FROM logs WHERE level='error' AND time > now() - 24h GROUP BY service`)
	mustContain(t, c.SQL,
		"SELECT service AS service, count() AS errors FROM logs WHERE tenant_id = 'T1' AND",
		"level = 'error'",
		"timestamp > now() - INTERVAL 86400 SECOND",
		"GROUP BY service",
	)
	if want := []string{"service", "errors"}; !equal(c.Columns, want) {
		t.Errorf("columns = %v, want %v", c.Columns, want)
	}
}

// TAD §10.2 auth example.
func TestAuthEventsFixture(t *testing.T) {
	c := compile(t, `SELECT tenant_id, count(*) AS failed_logins FROM auth_events WHERE auth.event_type = 'login_failed' AND time > now() - 1h GROUP BY tenant_id`)
	mustContain(t, c.SQL,
		"FROM auth_events WHERE tenant_id = 'T1' AND",
		"event_type = 'login_failed'",
		"count() AS failed_logins",
		"GROUP BY tenant_id",
	)
}

func TestTailRoutedAway(t *testing.T) {
	_, err := Compile(`TAIL FROM logs WHERE service = 'payments-api'`, "T1", Options{DefaultLimit: 100})
	if !errors.Is(err, ErrTail) {
		t.Fatalf("expected ErrTail, got %v", err)
	}
}

func TestTenantIsolationGuard(t *testing.T) {
	// Even an OR injection is confined by the outer forced tenant guard.
	c := compile(t, `SELECT * FROM logs WHERE tenant = 'evil' OR service = 'x'`)
	if !strings.HasPrefix(c.SQL, "SELECT id, timestamp, service, level, message, trace_id, span_id, body FROM logs WHERE tenant_id = 'T1' AND (") {
		t.Errorf("missing forced tenant guard: %s", c.SQL)
	}
	if strings.Contains(c.SQL, "evil") {
		t.Errorf("user tenant leaked: %s", c.SQL)
	}
}

func TestLevelSeverityOrdering(t *testing.T) {
	c := compile(t, `SELECT count(*) FROM logs WHERE level >= 'warn'`)
	mustContain(t, c.SQL,
		"indexOf(['trace','debug','info','warn','error','fatal'], level) >= indexOf(['trace','debug','info','warn','error','fatal'], 'warn')")
}

func TestBodyJSONExtract(t *testing.T) {
	c := compile(t, `SELECT body.user_id FROM logs WHERE body.region = 'eu'`)
	mustContain(t, c.SQL,
		"JSONExtractString(body, 'user_id') AS user_id",
		"JSONExtractString(body, 'region') = 'eu'",
	)
}

func TestStringEscaping(t *testing.T) {
	c := compile(t, `SELECT * FROM logs WHERE service = 'a\'b'`)
	mustContain(t, c.SQL, `service = 'a\'b'`)
}

func TestLimitClamp(t *testing.T) {
	c := compile(t, `SEARCH "x" FROM logs LIMIT 999999`)
	mustContain(t, c.SQL, "LIMIT 1000") // clamped to MaxLimit
}

func TestUnknownFieldRejected(t *testing.T) {
	if _, err := Compile(`SELECT bogusfield FROM logs`, "T1", Options{DefaultLimit: 100}); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestUnknownTableRejected(t *testing.T) {
	if _, err := Compile(`SELECT * FROM secrets`, "T1", Options{DefaultLimit: 100}); err == nil {
		t.Fatal("expected error for unknown table")
	}
}

func TestMatchLiveTail(t *testing.T) {
	rec := map[string]any{
		"service": "payments", "level": "error",
		"message": "boom", "body": `{"region":"eu"}`,
	}
	cases := []struct {
		q    string
		want bool
	}{
		{`TAIL FROM logs WHERE service = 'payments'`, true},
		{`TAIL FROM logs WHERE service = 'web'`, false},
		{`TAIL FROM logs WHERE level >= 'warn'`, true},  // error >= warn
		{`TAIL FROM logs WHERE level >= 'fatal'`, false}, // error < fatal
		{`TAIL FROM logs WHERE service = 'payments' AND level >= 'warn'`, true},
		{`TAIL FROM logs WHERE service = 'web' OR level = 'error'`, true},
		{`TAIL FROM logs WHERE body.region = 'eu'`, true},
		{`TAIL FROM logs WHERE body.region = 'us'`, false},
		{`TAIL FROM logs WHERE tenant = 'anything'`, true}, // tenant always satisfied
	}
	for _, tc := range cases {
		stmt, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.q, err)
		}
		if got := Match(stmt.Where, rec); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
