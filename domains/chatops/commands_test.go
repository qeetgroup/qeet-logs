package chatops

import (
	"encoding/json"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		text       string
		wantAction Action
		wantArg    string
	}{
		{"query level=error service=checkout", ActionQuery, "level=error service=checkout"},
		{"q SELECT * FROM logs", ActionQuery, "SELECT * FROM logs"},
		{"incidents", ActionIncidents, ""},
		{"inc", ActionIncidents, ""},
		{"rca payments", ActionRCA, "payments"},
		{"help", ActionHelp, ""},
		{"", ActionHelp, ""},
		{"  ", ActionHelp, ""},
		{"level=error", ActionQuery, "level=error"}, // bare query-ish text → query
		{"good morning", ActionHelp, ""},            // bare prose → help
	}
	for _, c := range cases {
		got := ParseCommand(c.text)
		if got.Action != c.wantAction || got.Arg != c.wantArg {
			t.Errorf("ParseCommand(%q) = {%q,%q}, want {%q,%q}",
				c.text, got.Action, got.Arg, c.wantAction, c.wantArg)
		}
	}
}

func TestSlashReply(t *testing.T) {
	var ephemeral slashResponse
	if err := json.Unmarshal(SlashReply("hi", false), &ephemeral); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ephemeral.ResponseType != "ephemeral" || ephemeral.Text != "hi" {
		t.Errorf("ephemeral reply = %+v", ephemeral)
	}

	var inChannel slashResponse
	_ = json.Unmarshal(SlashReply("broadcast", true), &inChannel)
	if inChannel.ResponseType != "in_channel" {
		t.Errorf("in-channel reply type = %q", inChannel.ResponseType)
	}
}

func TestHelpTextNonEmpty(t *testing.T) {
	if len(HelpText()) == 0 {
		t.Error("help text must not be empty")
	}
}
