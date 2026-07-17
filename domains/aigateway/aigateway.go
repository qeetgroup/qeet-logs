// Package aigateway is the governed-LLM substrate for the AI Copilot (PRD
// Module 12 / P2-G11). It is pure, dependency-free Go: no network, no database,
// no vendored model. It gives the copilot handler three governance primitives:
//
//   - MaskPII      — scrub emails / IPv4 / bearer+JWT tokens / long digit runs
//     out of a prompt BEFORE it ever leaves the process.
//   - LLM          — a one-method interface so the (real) Anthropic call is
//     injectable and the whole flow is unit-testable without a network.
//   - Govern       — the opt-in-gate → mask → call → audit-entry pipeline. It
//     refuses to touch the LLM for a tenant that has not opted in, and the
//     prompt it sends (and records) is always masked.
//
// The LLM behind the interface is the EXISTING Anthropic Messages call the
// platform already uses for NL-to-query — this package adds no new model.
package aigateway

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// FeatureCopilot is the ai_decision_log.feature value for copilot invocations.
const FeatureCopilot = "copilot"

// ErrNotEnabled is returned by Govern when the tenant has not opted in. It is the
// signal the handler maps to HTTP 403 — the LLM is never called in this case.
var ErrNotEnabled = errors.New("ai features not enabled for tenant")

// Result is the governed LLM output surfaced to the caller: an inspectable,
// editable LogQL++ query plus a plain-language explanation.
type Result struct {
	LogQLPP     string
	Explanation string
}

// LLM is the injectable completion backend. Production wires in the Anthropic
// Messages call (see platform/api/handler/copilot.go); tests inject a stub, so
// the governance flow is exercised end-to-end with zero network.
type LLM interface {
	Complete(ctx context.Context, prompt string) (Result, error)
}

// AuditEntry is the row builder for ai_decision_log — the governance trail. It
// only ever carries the MASKED prompt; raw user text never reaches it.
type AuditEntry struct {
	TenantID        string
	Feature         string
	PromptMasked    string
	ResponseSummary string
	Model           string
}

// Request is one governed copilot invocation. Enabled is the tenant's resolved
// ai_features.ai_features_enabled flag — the opt-in gate.
type Request struct {
	TenantID string
	Enabled  bool
	Feature  string
	Question string
	Context  string
	Model    string
}

// Govern runs the full governance pipeline:
//
//	opt-in gate → PII-mask → LLM call → audit-entry build
//
// It NEVER calls llm for a tenant with Enabled == false (returns ErrNotEnabled).
// The returned AuditEntry is always populated with the masked prompt + model —
// even on LLM error — so the caller can record the attempt. It is pure apart
// from the injected llm.
func Govern(ctx context.Context, req Request, llm LLM) (Result, AuditEntry, error) {
	if !req.Enabled {
		return Result{}, AuditEntry{}, ErrNotEnabled
	}

	masked := MaskPII(strings.TrimSpace(req.Question))
	if strings.TrimSpace(req.Context) != "" {
		masked = masked + "\n\ncontext: " + MaskPII(req.Context)
	}

	entry := AuditEntry{
		TenantID:     req.TenantID,
		Feature:      req.Feature,
		PromptMasked: masked,
		Model:        req.Model,
	}

	res, err := llm.Complete(ctx, masked)
	if err != nil {
		return Result{}, entry, err
	}
	entry.ResponseSummary = summarize(res)
	return res, entry, nil
}

// summarize produces a short, PII-free digest of the model decision for the
// audit trail. The generated LogQL++ never contains tenant data (tenant_id is
// injected downstream, never by the model), so it is safe to record verbatim.
func summarize(r Result) string {
	s := strings.TrimSpace(r.LogQLPP)
	if s == "" {
		s = strings.TrimSpace(r.Explanation)
	}
	return truncate(s, 240)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- PII masking ------------------------------------------------------------
//
// Phase-0 regex detectors, mirroring the ingest-side PII gate philosophy
// (synchronous, pre-egress). Applied in an order that keeps compound secrets
// (a bearer header wrapping a JWT) from being split mid-token, and leaves the
// inserted placeholders (which contain no digits) untouched by later passes.

var (
	// "Bearer <token>" / "bearer <token>" — mask the whole credential.
	reBearer = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	// Bare JWT-ish triple (header.payload.signature).
	reJWT = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	// Email addresses.
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// IPv4 dotted-quad.
	reIPv4 = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// Long digit runs (7+) — phone / card / account-ish. Short numbers (ports,
	// "last 3600 seconds") are preserved so query intent survives masking.
	reDigits = regexp.MustCompile(`\b\d{7,}\b`)
)

// MaskPII replaces likely PII in s with typed placeholders. It is pure and
// deterministic, safe to call on any string, and never panics.
func MaskPII(s string) string {
	s = reBearer.ReplaceAllString(s, "Bearer [REDACTED]")
	s = reJWT.ReplaceAllString(s, "[JWT]")
	s = reEmail.ReplaceAllString(s, "[EMAIL]")
	s = reIPv4.ReplaceAllString(s, "[IP]")
	s = reDigits.ReplaceAllString(s, "[NUM]")
	return s
}
