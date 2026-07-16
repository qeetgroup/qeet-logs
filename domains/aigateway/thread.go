package aigateway

import (
	"fmt"
	"strings"
)

// Multi-turn conversation support for the AI Copilot (PRD Module 12.2 / P2-G11).
// The governed LLM call (Govern + the injected LLM) is single-message; this
// helper assembles a bounded conversation history plus the new question into one
// prompt string so a follow-up ("now only 5xx", "scope that to prod") carries
// its context. It is pure — no I/O — and the assembled text still flows through
// Govern's PII-masking and audit like any other prompt.

// Turn is one stored conversation turn. Assistant turns carry the generated
// LogQL++ in LogQLPP; user turns carry only Content.
type Turn struct {
	Role    string // "user" | "assistant"
	Content string // user question, or assistant explanation
	LogQLPP string // assistant turns only: the generated query
}

// DefaultHistoryTurns bounds how many prior turns are replayed into a prompt.
const DefaultHistoryTurns = 10

// BuildThreadPrompt renders the last maxTurns of history followed by the new
// question into a single prompt. When maxTurns <= 0 it uses DefaultHistoryTurns.
// With no history it returns just the question, so the first turn is identical
// to the single-shot copilot path. Pure and deterministic.
func BuildThreadPrompt(history []Turn, question string, maxTurns int) string {
	if maxTurns <= 0 {
		maxTurns = DefaultHistoryTurns
	}
	if len(history) > maxTurns {
		history = history[len(history)-maxTurns:]
	}
	q := strings.TrimSpace(question)
	if len(history) == 0 {
		return q
	}

	var b strings.Builder
	b.WriteString("This is a multi-turn conversation. Prior turns:\n")
	for _, t := range history {
		switch t.Role {
		case "assistant":
			line := strings.TrimSpace(t.LogQLPP)
			if line == "" {
				line = strings.TrimSpace(t.Content)
			}
			fmt.Fprintf(&b, "Assistant: %s\n", line)
		default:
			fmt.Fprintf(&b, "User: %s\n", strings.TrimSpace(t.Content))
		}
	}
	b.WriteString("\nBuild on the conversation above. New question:\n")
	b.WriteString("User: " + q)
	return b.String()
}
