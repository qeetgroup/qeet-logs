package query

import (
	"strings"
	"testing"
)

const testTenant = "t-123"

func TestCompileMetricsAndTraces(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string // substrings that must be present in the SQL
		wantErr bool
	}{
		{
			name:  "select from metrics with agg + attr label",
			input: `SELECT service, avg(value) FROM metrics WHERE metric_name = 'http.server.duration' AND attr.route = '/charge' GROUP BY service`,
			want:  []string{"FROM metrics", "avg(value)", "attributes['route'] = '/charge'", "tenant_id = 't-123'", "GROUP BY service"},
		},
		{
			name:  "select from traces filters + resource extract",
			input: `SELECT service, name, duration_ns FROM traces WHERE status_code = 'error' AND resource.host = 'pod-a' ORDER BY duration_ns DESC LIMIT 10`,
			want:  []string{"FROM traces", "status_code = 'error'", "JSONExtractString(resource, 'host') = 'pod-a'", "ORDER BY duration_ns DESC"},
		},
		{
			name:    "search rejected on metrics",
			input:   `SEARCH "boom" FROM metrics`,
			wantErr: true,
		},
		{
			name:    "unknown metrics field rejected",
			input:   `SELECT nonsense FROM metrics`,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(tc.input, testTenant, Options{DefaultLimit: 100, MaxLimit: 1000})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got SQL: %s", c.SQL)
				}
				return
			}
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for _, sub := range tc.want {
				if !strings.Contains(c.SQL, sub) {
					t.Errorf("SQL missing %q\n got: %s", sub, c.SQL)
				}
			}
		})
	}
}

func TestCompilePromQL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		params  PromParams
		want    []string
		labels  []string
		wantErr bool
	}{
		{
			name:   "bare selector instant",
			input:  `process_memory_usage{host="pod-a"}`,
			params: PromParams{EndUnix: 1_752_000_000, Lookback: 300},
			want: []string{
				"FROM metrics", "metric_name = 'process_memory_usage'",
				"attributes['host'] = 'pod-a'", "tenant_id = 't-123'",
				"argMax(value, timestamp)", "GROUP BY service, environment, attributes, bucket",
			},
			labels: []string{"service", "environment"},
		},
		{
			name:   "sum by service range",
			input:  `sum by (service) (http_server_requests)`,
			params: PromParams{StartUnix: 1_752_000_000, EndUnix: 1_752_003_600, StepSec: 60},
			want:   []string{"sum(v)", "service AS `service`", "GROUP BY `service`, bucket", "argMax(value, timestamp)"},
			labels: []string{"service"},
		},
		{
			name:   "rate of counter",
			input:  `rate(http_server_requests[5m])`,
			params: PromParams{EndUnix: 1_752_000_000, Lookback: 300},
			want:   []string{"(max(value) - min(value)) / 300"},
		},
		{
			name:   "regex matcher anchored",
			input:  `http_server_requests{route=~"/charge.*"}`,
			params: PromParams{EndUnix: 1_752_000_000, Lookback: 300},
			want:   []string{`match(attributes['route'], '^(?:/charge.*)$')`},
		},
		{
			name:    "empty query rejected",
			input:   ``,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := CompilePromQL(tc.input, testTenant, tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got SQL: %s", c.SQL)
				}
				return
			}
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for _, sub := range tc.want {
				if !strings.Contains(c.SQL, sub) {
					t.Errorf("SQL missing %q\n got: %s", sub, c.SQL)
				}
			}
			for _, l := range tc.labels {
				found := false
				for _, got := range c.LabelCols {
					if got == l {
						found = true
					}
				}
				if !found {
					t.Errorf("expected label col %q in %v", l, c.LabelCols)
				}
			}
		})
	}
}

// Guard: the tenant predicate is always injected and never overridable.
func TestPromQLForcesTenant(t *testing.T) {
	c, err := CompilePromQL(`up{__name__="evil"}`, testTenant, PromParams{EndUnix: 1, Lookback: 300})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.SQL, "tenant_id = 't-123'") {
		t.Fatalf("tenant guard missing: %s", c.SQL)
	}
}
