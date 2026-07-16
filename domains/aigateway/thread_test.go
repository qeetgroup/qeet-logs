package aigateway

import (
	"strings"
	"testing"
)

func TestBuildThreadPromptNoHistory(t *testing.T) {
	// First turn must be identical to the single-shot path: just the question.
	got := BuildThreadPrompt(nil, "  errors from checkout  ", 10)
	if got != "errors from checkout" {
		t.Errorf("no-history prompt = %q, want the trimmed question", got)
	}
}

func TestBuildThreadPromptIncludesHistoryAndQuestion(t *testing.T) {
	history := []Turn{
		{Role: "user", Content: "errors from checkout"},
		{Role: "assistant", Content: "checkout errors", LogQLPP: "SELECT * FROM logs WHERE service='checkout' AND level='error'"},
	}
	got := BuildThreadPrompt(history, "now only in prod", 10)

	if !strings.Contains(got, "User: errors from checkout") {
		t.Errorf("missing prior user turn:\n%s", got)
	}
	if !strings.Contains(got, "Assistant: SELECT * FROM logs WHERE service='checkout'") {
		t.Errorf("assistant turn should replay the LogQL++:\n%s", got)
	}
	if !strings.Contains(got, "User: now only in prod") {
		t.Errorf("missing the new question:\n%s", got)
	}
}

func TestBuildThreadPromptCapsHistory(t *testing.T) {
	var history []Turn
	for i := 0; i < 50; i++ {
		history = append(history, Turn{Role: "user", Content: "q" + string(rune('a'+i%26))})
	}
	got := BuildThreadPrompt(history, "latest", 3)
	// Only the last 3 turns replayed → at most 3 "User:" history lines + 1 new.
	if n := strings.Count(got, "User:"); n > 4 {
		t.Errorf("history not capped: %d User lines\n%s", n, got)
	}
}

func TestBuildThreadPromptDefaultCap(t *testing.T) {
	got := BuildThreadPrompt([]Turn{{Role: "user", Content: "x"}}, "y", 0)
	if !strings.Contains(got, "User: x") || !strings.Contains(got, "User: y") {
		t.Errorf("maxTurns<=0 should default, got:\n%s", got)
	}
}
