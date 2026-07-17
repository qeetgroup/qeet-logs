// Package changesource translates third-party CI/CD, feature-flag, and config
// webhook payloads into the normalized Qeet Logs change-event contract (PRD
// Module 15.1 GA / 30.4 inbound / 31.3 CI-CD / 31.4 feature-flag connectors).
//
// The output feeds the Deployment Intelligence layer (Module 15, domains/deploy):
// once a GitHub deploy or a LaunchDarkly flag flip lands as a change_event, it is
// automatically ranked as a potential culprit for any regression that follows.
// Parsers are pure and tolerant — missing fields degrade to empty strings rather
// than erroring, so a partial payload still records "something changed".
package changesource

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event is one normalized change event (mirrors the change_events columns).
type Event struct {
	Service     string
	Environment string
	Kind        string // deploy | flag | config | rollback
	Title       string
	GitSHA      string
	DeployID    string
	PRNumber    string
	FlagKey     string
	ConfigDiff  string
	Author      string
}

// Parse dispatches to the provider-specific parser. eventType is the provider's
// event-type hint (e.g. GitHub's X-GitHub-Event header); it is ignored by
// providers that self-describe their payload (GitLab object_kind, LaunchDarkly).
func Parse(provider, eventType string, payload []byte) ([]Event, error) {
	switch strings.ToLower(provider) {
	case "github":
		return parseGitHub(eventType, payload)
	case "gitlab":
		return parseGitLab(payload)
	case "launchdarkly", "ld":
		return parseLaunchDarkly(payload)
	default:
		return nil, fmt.Errorf("unknown change-event provider %q", provider)
	}
}

// ── GitHub (deployment, workflow_run, push) ─────────────────────────────────────

func parseGitHub(eventType string, payload []byte) ([]Event, error) {
	var p struct {
		Action     string `json:"action"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			Name string `json:"name"`
		} `json:"repository"`
		Pusher struct {
			Name string `json:"name"`
		} `json:"pusher"`
		HeadCommit *struct {
			ID string `json:"id"`
		} `json:"head_commit"`
		Deployment *struct {
			ID          json.Number `json:"id"`
			SHA         string      `json:"sha"`
			Ref         string      `json:"ref"`
			Environment string      `json:"environment"`
			Creator     struct {
				Login string `json:"login"`
			} `json:"creator"`
		} `json:"deployment"`
		WorkflowRun *struct {
			ID         json.Number `json:"id"`
			Name       string      `json:"name"`
			HeadSHA    string      `json:"head_sha"`
			HeadBranch string      `json:"head_branch"`
			Conclusion string      `json:"conclusion"`
			Actor      struct {
				Login string `json:"login"`
			} `json:"actor"`
		} `json:"workflow_run"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("github payload: %w", err)
	}
	svc := p.Repository.Name

	switch eventType {
	case "deployment":
		if p.Deployment == nil {
			return nil, nil
		}
		d := p.Deployment
		return []Event{{
			Service: svc, Environment: d.Environment, Kind: "deploy",
			GitSHA: d.SHA, DeployID: d.ID.String(), Author: d.Creator.Login,
			Title: fmt.Sprintf("Deployment of %s to %s", refName(d.Ref), orDash(d.Environment)),
		}}, nil
	case "workflow_run":
		if p.WorkflowRun == nil || p.WorkflowRun.Conclusion != "success" {
			return nil, nil // only successful runs are deploy markers
		}
		wr := p.WorkflowRun
		return []Event{{
			Service: svc, Kind: "deploy", GitSHA: wr.HeadSHA, DeployID: wr.ID.String(),
			Author: wr.Actor.Login,
			Title:  fmt.Sprintf("Workflow %q succeeded on %s", wr.Name, refName(wr.HeadBranch)),
		}}, nil
	case "push":
		sha := p.After
		if sha == "" && p.HeadCommit != nil {
			sha = p.HeadCommit.ID
		}
		if sha == "" {
			return nil, nil
		}
		return []Event{{
			Service: svc, Kind: "deploy", GitSHA: sha, Author: p.Pusher.Name,
			Title: fmt.Sprintf("Push to %s", refName(p.Ref)),
		}}, nil
	default:
		return nil, nil // unsupported event type — ignored, not an error
	}
}

// ── GitLab (pipeline, deployment) ───────────────────────────────────────────────

func parseGitLab(payload []byte) ([]Event, error) {
	var p struct {
		ObjectKind   string      `json:"object_kind"`
		Ref          string      `json:"ref"`
		Status       string      `json:"status"`
		SHA          string      `json:"sha"`
		Environment  string      `json:"environment"`
		DeployableID json.Number `json:"deployable_id"`
		Project      struct {
			Name string `json:"name"`
		} `json:"project"`
		User struct {
			Name string `json:"name"`
		} `json:"user"`
		ObjectAttributes *struct {
			SHA    string `json:"sha"`
			Ref    string `json:"ref"`
			Status string `json:"status"`
		} `json:"object_attributes"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("gitlab payload: %w", err)
	}
	svc := p.Project.Name

	switch p.ObjectKind {
	case "pipeline":
		if p.ObjectAttributes == nil || p.ObjectAttributes.Status != "success" {
			return nil, nil
		}
		oa := p.ObjectAttributes
		return []Event{{
			Service: svc, Kind: "deploy", GitSHA: oa.SHA, Author: p.User.Name,
			Title: fmt.Sprintf("Pipeline succeeded on %s", refName(oa.Ref)),
		}}, nil
	case "deployment":
		if p.Status != "success" {
			return nil, nil
		}
		return []Event{{
			Service: svc, Environment: p.Environment, Kind: "deploy", GitSHA: p.SHA,
			DeployID: p.DeployableID.String(), Author: p.User.Name,
			Title: fmt.Sprintf("Deployment to %s", orDash(p.Environment)),
		}}, nil
	default:
		return nil, nil
	}
}

// ── LaunchDarkly (flag change) ──────────────────────────────────────────────────

func parseLaunchDarkly(payload []byte) ([]Event, error) {
	var p struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Title     string `json:"title"`
		TitleVerb string `json:"titleVerb"`
		Member    struct {
			Email string `json:"email"`
		} `json:"member"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("launchdarkly payload: %w", err)
	}
	title := p.Title
	if title == "" {
		title = strings.TrimSpace(fmt.Sprintf("Flag %s %s", p.Name, p.TitleVerb))
	}
	return []Event{{
		Kind: "flag", FlagKey: p.Name, Title: title, Author: p.Member.Email,
	}}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// refName strips the refs/heads/ or refs/tags/ prefix from a git ref.
func refName(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	if ref == "" {
		return "(unknown)"
	}
	return ref
}

func orDash(s string) string {
	if s == "" {
		return "(default)"
	}
	return s
}
