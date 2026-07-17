package aigateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMaskPII(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		absent  []string // substrings that must NOT survive masking
		present []string // substrings that must survive (semantics preserved)
	}{
		{
			name: "email",
			in:   "errors for user alice.smith@example.co.uk in the last hour",
			want: "errors for user [EMAIL] in the last hour",
		},
		{
			name: "ipv4",
			in:   "requests from 192.168.10.254 failing",
			want: "requests from [IP] failing",
		},
		{
			name: "bearer token",
			in:   "Authorization: Bearer abc123.DEF_456-ghi in header",
			want: "Authorization: Bearer [REDACTED] in header",
		},
		{
			name: "bare jwt",
			in:   "token eyJhbGciOiJI.eyJzdWIiOiIxMjM.SflKxwRJSMeKKF2Q was set",
			want: "token [JWT] was set",
		},
		{
			name:   "long digit run masked",
			in:     "account 12345678901 saw errors",
			absent: []string{"12345678901"},
		},
		{
			name:    "short number preserved",
			in:      "errors in the last 3600 seconds on port 8100",
			want:    "errors in the last 3600 seconds on port 8100",
			present: []string{"3600", "8100"},
		},
		{
			name:    "clean text untouched",
			in:      "SELECT * FROM logs WHERE service = 'payments'",
			want:    "SELECT * FROM logs WHERE service = 'payments'",
			present: []string{"payments"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MaskPII(c.in)
			if c.want != "" && got != c.want {
				t.Errorf("MaskPII(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
			for _, a := range c.absent {
				if strings.Contains(got, a) {
					t.Errorf("MaskPII(%q) leaked %q: %q", c.in, a, got)
				}
			}
			for _, p := range c.present {
				if !strings.Contains(got, p) {
					t.Errorf("MaskPII(%q) dropped %q: %q", c.in, p, got)
				}
			}
		})
	}
}

func TestMaskPII_MultiplePIIInOnePrompt(t *testing.T) {
	in := "user bob@corp.io from 10.0.0.1 hit 429s; header Bearer sk-tok_ABC.def"
	got := MaskPII(in)
	for _, leak := range []string{"bob@corp.io", "10.0.0.1", "sk-tok_ABC.def"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	for _, want := range []string{"[EMAIL]", "[IP]", "Bearer [REDACTED]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing placeholder %q in %q", want, got)
		}
	}
}

// stubLLM records the prompt it was handed and returns a canned result/error.
type stubLLM struct {
	calls     int
	gotPrompt string
	result    Result
	err       error
}

func (s *stubLLM) Complete(_ context.Context, prompt string) (Result, error) {
	s.calls++
	s.gotPrompt = prompt
	return s.result, s.err
}

func TestGovern_NotEnabled_NeverCallsLLM(t *testing.T) {
	llm := &stubLLM{result: Result{LogQLPP: "SELECT 1"}}
	req := Request{TenantID: "t1", Enabled: false, Feature: FeatureCopilot, Question: "anything"}

	_, entry, err := Govern(context.Background(), req, llm)
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("want ErrNotEnabled, got %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("opt-in gate breached: LLM called %d times", llm.calls)
	}
	if entry != (AuditEntry{}) {
		t.Errorf("no audit entry expected for gated request, got %+v", entry)
	}
}

func TestGovern_MasksBeforeCall(t *testing.T) {
	llm := &stubLLM{result: Result{LogQLPP: "SELECT * FROM logs", Explanation: "ok"}}
	req := Request{
		TenantID: "t1",
		Enabled:  true,
		Feature:  FeatureCopilot,
		Question: "why did alice@example.com from 192.168.0.9 get errors",
		Model:    "claude-sonnet-5",
	}

	res, entry, err := Govern(context.Background(), req, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llm.calls != 1 {
		t.Fatalf("want 1 LLM call, got %d", llm.calls)
	}
	// The prompt the LLM actually received must be masked.
	if strings.Contains(llm.gotPrompt, "alice@example.com") || strings.Contains(llm.gotPrompt, "192.168.0.9") {
		t.Errorf("raw PII reached LLM: %q", llm.gotPrompt)
	}
	// The audit entry must mirror exactly what was sent, and carry the model.
	if entry.PromptMasked != llm.gotPrompt {
		t.Errorf("audit prompt %q != sent prompt %q", entry.PromptMasked, llm.gotPrompt)
	}
	if entry.Model != "claude-sonnet-5" {
		t.Errorf("audit model = %q, want claude-sonnet-5", entry.Model)
	}
	if entry.Feature != FeatureCopilot {
		t.Errorf("audit feature = %q, want %q", entry.Feature, FeatureCopilot)
	}
	if entry.ResponseSummary == "" {
		t.Errorf("audit response_summary should be populated on success")
	}
	if res.LogQLPP != "SELECT * FROM logs" {
		t.Errorf("result not surfaced: %+v", res)
	}
}

func TestGovern_MasksContextToo(t *testing.T) {
	llm := &stubLLM{result: Result{LogQLPP: "SELECT 1"}}
	req := Request{
		TenantID: "t1",
		Enabled:  true,
		Feature:  FeatureCopilot,
		Question: "investigate",
		Context:  "seen on host 10.11.12.13 for user carol@x.io",
	}
	_, _, err := Govern(context.Background(), req, llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(llm.gotPrompt, "10.11.12.13") || strings.Contains(llm.gotPrompt, "carol@x.io") {
		t.Errorf("context PII reached LLM: %q", llm.gotPrompt)
	}
}

func TestGovern_LLMErrorStillBuildsMaskedEntry(t *testing.T) {
	llm := &stubLLM{err: errors.New("anthropic 502")}
	req := Request{
		TenantID: "t1",
		Enabled:  true,
		Feature:  FeatureCopilot,
		Question: "user dave@d.io failing",
		Model:    "claude-sonnet-5",
	}

	_, entry, err := Govern(context.Background(), req, llm)
	if err == nil {
		t.Fatalf("want LLM error propagated")
	}
	// Even on error we must be able to audit the (masked) attempt.
	if entry.PromptMasked == "" || strings.Contains(entry.PromptMasked, "dave@d.io") {
		t.Errorf("audit entry should carry masked prompt on error, got %q", entry.PromptMasked)
	}
	if entry.Model != "claude-sonnet-5" {
		t.Errorf("audit model should be set on error, got %q", entry.Model)
	}
	if entry.ResponseSummary != "" {
		t.Errorf("no response summary on error, got %q", entry.ResponseSummary)
	}
}
