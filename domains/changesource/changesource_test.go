package changesource

import "testing"

func TestParseGitHubDeployment(t *testing.T) {
	payload := []byte(`{
		"action":"created",
		"deployment":{"id":42,"sha":"abc123","ref":"main","environment":"production","creator":{"login":"alice"}},
		"repository":{"name":"checkout"}
	}`)
	evs, err := Parse("github", "deployment", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	e := evs[0]
	if e.Kind != "deploy" || e.Service != "checkout" || e.GitSHA != "abc123" ||
		e.DeployID != "42" || e.Environment != "production" || e.Author != "alice" {
		t.Errorf("unexpected event: %+v", e)
	}
}

func TestParseGitHubWorkflowRun(t *testing.T) {
	ok := []byte(`{"action":"completed","repository":{"name":"api"},
		"workflow_run":{"id":7,"name":"Deploy","head_sha":"def","head_branch":"main","conclusion":"success","actor":{"login":"bob"}}}`)
	evs, _ := Parse("github", "workflow_run", ok)
	if len(evs) != 1 || evs[0].GitSHA != "def" || evs[0].Kind != "deploy" || evs[0].DeployID != "7" {
		t.Fatalf("success run: unexpected %+v", evs)
	}
	// A failed run is not a deploy marker → no events.
	fail := []byte(`{"action":"completed","workflow_run":{"conclusion":"failure"}}`)
	evs, _ = Parse("github", "workflow_run", fail)
	if len(evs) != 0 {
		t.Errorf("failed run should produce no events, got %+v", evs)
	}
}

func TestParseGitHubPush(t *testing.T) {
	payload := []byte(`{"ref":"refs/heads/main","after":"cafe","repository":{"name":"web"},"pusher":{"name":"carol"}}`)
	evs, _ := Parse("github", "push", payload)
	if len(evs) != 1 || evs[0].GitSHA != "cafe" || evs[0].Author != "carol" {
		t.Fatalf("push: unexpected %+v", evs)
	}
}

func TestParseGitLab(t *testing.T) {
	// Pipeline success.
	pipe := []byte(`{"object_kind":"pipeline","project":{"name":"payments"},"user":{"name":"dan"},
		"object_attributes":{"sha":"111","ref":"main","status":"success"}}`)
	evs, _ := Parse("gitlab", "", pipe)
	if len(evs) != 1 || evs[0].GitSHA != "111" || evs[0].Service != "payments" || evs[0].Kind != "deploy" {
		t.Fatalf("pipeline: unexpected %+v", evs)
	}
	// Non-success pipeline → no events.
	running := []byte(`{"object_kind":"pipeline","object_attributes":{"status":"running"}}`)
	if evs, _ := Parse("gitlab", "", running); len(evs) != 0 {
		t.Errorf("running pipeline should produce no events, got %+v", evs)
	}
	// Deployment success.
	dep := []byte(`{"object_kind":"deployment","status":"success","sha":"222","environment":"prod","deployable_id":9,"project":{"name":"billing"},"user":{"name":"eve"}}`)
	evs, _ = Parse("gitlab", "", dep)
	if len(evs) != 1 || evs[0].DeployID != "9" || evs[0].Environment != "prod" || evs[0].GitSHA != "222" {
		t.Fatalf("deployment: unexpected %+v", evs)
	}
}

func TestParseLaunchDarkly(t *testing.T) {
	payload := []byte(`{"kind":"flag","name":"new-checkout","title":"Alice turned on new-checkout in Production","member":{"email":"alice@x.com"}}`)
	evs, err := Parse("launchdarkly", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != "flag" || evs[0].FlagKey != "new-checkout" || evs[0].Author != "alice@x.com" {
		t.Fatalf("launchdarkly: unexpected %+v", evs)
	}
}

func TestParseUnknownProvider(t *testing.T) {
	if _, err := Parse("bitbucket", "", []byte(`{}`)); err == nil {
		t.Error("expected error for unknown provider")
	}
}
