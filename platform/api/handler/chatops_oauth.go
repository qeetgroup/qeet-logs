package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/chatops"
	"github.com/qeetgroup/qeet-logs/domains/query"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
	"github.com/qeetgroup/qeet-logs/platform/security"
)

// Two-way ChatOps: Slack OAuth app install + slash-commands (PRD Module 19.1 /
// 19.3 / P2-G7). This is the INBOUND half — the outbound one-way delivery lives
// in handler/chatops.go. It is REAL Slack integration code: it drives the OAuth
// v2 flow, verifies Slack's request signature, resolves the calling workspace
// back to a Qeet tenant via chatops_installations, and executes the command
// against the same query stack the REST API uses. It needs a registered Slack
// app + secrets (SLACK_CLIENT_ID / SLACK_CLIENT_SECRET / SLACK_SIGNING_SECRET,
// and SLACK_REDIRECT_URL) to run; every entry point returns 501 when its
// required secret is unset, and NEVER fabricates a Slack response.
//
// All identifiers are prefixed chatopsSlack* / slackOAuth* so this file never
// collides with handler/chatops.go.

const (
	slackAuthorizeURL = "https://slack.com/oauth/v2/authorize"
	slackTokenURL     = "https://slack.com/api/oauth.v2.access"
	// Bot scopes the app requests: run slash-commands and post replies.
	slackScopes = "commands,chat:write"
	// Reject requests whose timestamp is older than this (replay protection).
	slackMaxSkew = 5 * time.Minute
)

var slackHTTP = &http.Client{Timeout: 10 * time.Second}

// ChatOpsSlackInstall handles GET /v1/chatops/slack/install?tenant=<uuid>. It
// redirects the admin's browser into Slack's OAuth consent screen, carrying the
// tenant id as the OAuth `state`. 501 if SLACK_CLIENT_ID is unset.
//
// SECURITY NOTE: for the dev substrate `state` is the raw tenant id. In
// production `state` MUST be a signed, single-use nonce minted by an
// authenticated admin endpoint, so a workspace cannot be bound to a tenant it
// does not own. That signing is the gated hardening step.
func ChatOpsSlackInstall() http.HandlerFunc {
	clientID := os.Getenv("SLACK_CLIENT_ID")
	redirectURI := os.Getenv("SLACK_REDIRECT_URL")
	return func(w http.ResponseWriter, r *http.Request) {
		if clientID == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Slack app not configured (SLACK_CLIENT_ID unset)"})
			return
		}
		tenant := r.URL.Query().Get("tenant")
		if tenant == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant query parameter is required"})
			return
		}
		q := url.Values{}
		q.Set("client_id", clientID)
		q.Set("scope", slackScopes)
		q.Set("state", tenant)
		if redirectURI != "" {
			q.Set("redirect_uri", redirectURI)
		}
		http.Redirect(w, r, slackAuthorizeURL+"?"+q.Encode(), http.StatusFound)
	}
}

// ChatOpsSlackCallback handles GET /v1/chatops/slack/callback?code=&state=. It
// exchanges the authorization code for a bot token via oauth.v2.access and
// persists the workspace→tenant installation. 501 if the client secret is unset.
func ChatOpsSlackCallback(pool *pgxpool.Pool) http.HandlerFunc {
	clientID := os.Getenv("SLACK_CLIENT_ID")
	clientSecret := os.Getenv("SLACK_CLIENT_SECRET")
	redirectURI := os.Getenv("SLACK_REDIRECT_URL")
	return func(w http.ResponseWriter, r *http.Request) {
		if clientID == "" || clientSecret == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Slack app not configured (SLACK_CLIENT_ID / SLACK_CLIENT_SECRET unset)"})
			return
		}
		code := r.URL.Query().Get("code")
		tenant := r.URL.Query().Get("state")
		if code == "" || tenant == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and state are required"})
			return
		}

		form := url.Values{}
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
		form.Set("code", code)
		if redirectURI != "" {
			form.Set("redirect_uri", redirectURI)
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, slackTokenURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := slackHTTP.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "slack token exchange failed: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)

		var tok struct {
			OK          bool   `json:"ok"`
			Error       string `json:"error"`
			AccessToken string `json:"access_token"`
			Team        struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"team"`
			AuthedUser struct {
				ID string `json:"id"`
			} `json:"authed_user"`
		}
		if err := json.Unmarshal(raw, &tok); err != nil || !tok.OK {
			msg := tok.Error
			if msg == "" {
				msg = "unexpected response from slack"
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "slack oauth: " + msg})
			return
		}

		// Encrypt the workspace bot token at rest (AES-GCM envelope when
		// QEET_LOGS_SECRETS_KEY is set; dev passthrough otherwise).
		encToken, err := security.EncryptSecret(tok.AccessToken)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot secure bot token: " + err.Error()})
			return
		}
		_, err = pool.Exec(r.Context(),
			`INSERT INTO chatops_installations (tenant_id, provider, team_id, team_name, bot_token, installed_by)
			 VALUES ($1::uuid, 'slack', $2, $3, $4, $5)
			 ON CONFLICT (tenant_id, provider, team_id)
			 DO UPDATE SET team_name = EXCLUDED.team_name, bot_token = EXCLUDED.bot_token, installed_by = EXCLUDED.installed_by`,
			tenant, tok.Team.ID, tok.Team.Name, encToken, tok.AuthedUser.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"installed": true, "provider": "slack", "team": tok.Team.Name})
	}
}

// ChatOpsSlackCommands handles POST /v1/chatops/slack/commands — an incoming
// slash-command. It verifies Slack's request signature, resolves the workspace
// to a tenant, parses the command, executes it against the query stack, and
// replies with a Slack slash-command payload. 501 if SLACK_SIGNING_SECRET unset.
func ChatOpsSlackCommands(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	signingSecret := os.Getenv("SLACK_SIGNING_SECRET")
	return func(w http.ResponseWriter, r *http.Request) {
		if signingSecret == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Slack app not configured (SLACK_SIGNING_SECRET unset)"})
			return
		}
		body, ok := slackVerifiedBody(w, r, signingSecret)
		if !ok {
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form body"})
			return
		}
		teamID := form.Get("team_id")
		tenant := chatopsResolveTenant(r.Context(), pool, teamID)
		if tenant == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(chatops.SlashReply("This Slack workspace is not linked to a Qeet Logs tenant. Ask an admin to install the app.", false))
			return
		}

		cmd := chatops.ParseCommand(form.Get("text"))
		reply := chatopsExecute(r.Context(), ch, pool, tenant, cmd)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	}
}

// ChatOpsSlackInteractivity handles POST /v1/chatops/slack/interactivity —
// Block Kit button/menu callbacks. It verifies the signature and acks; wiring
// specific interactive actions is the next step. 501 if signing secret unset.
func ChatOpsSlackInteractivity() http.HandlerFunc {
	signingSecret := os.Getenv("SLACK_SIGNING_SECRET")
	return func(w http.ResponseWriter, r *http.Request) {
		if signingSecret == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Slack app not configured (SLACK_SIGNING_SECRET unset)"})
			return
		}
		if _, ok := slackVerifiedBody(w, r, signingSecret); !ok {
			return
		}
		w.WriteHeader(http.StatusOK) // ack; Slack shows nothing further
	}
}

// --- helpers ----------------------------------------------------------------

// slackVerifiedBody reads the raw request body and verifies Slack's v0 request
// signature (HMAC-SHA256 over "v0:{timestamp}:{body}"). On any failure it writes
// a 401 and returns ok=false. On success it returns the raw body.
func slackVerifiedBody(w http.ResponseWriter, r *http.Request, secret string) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read body"})
		return nil, false
	}
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")
	if !verifySlackSignature(secret, ts, sig, body) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid slack signature"})
		return nil, false
	}
	return body, true
}

// verifySlackSignature validates the Slack request signature and rejects stale
// timestamps (replay protection). Constant-time compare.
func verifySlackSignature(secret, timestamp, signature string, body []byte) bool {
	if secret == "" || timestamp == "" || signature == "" {
		return false
	}
	tsInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if d := time.Since(time.Unix(tsInt, 0)); d > slackMaxSkew || d < -slackMaxSkew {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// chatopsResolveTenant maps a Slack workspace (team_id) to its tenant via the
// installation record. Returns "" if the workspace is not linked.
func chatopsResolveTenant(ctx context.Context, pool *pgxpool.Pool, teamID string) string {
	if teamID == "" {
		return ""
	}
	var tenant string
	err := pool.QueryRow(ctx,
		`SELECT tenant_id::text FROM chatops_installations
		 WHERE provider = 'slack' AND team_id = $1 LIMIT 1`, teamID).Scan(&tenant)
	if err != nil {
		return ""
	}
	return tenant
}

// chatopsExecute runs the parsed command for the resolved tenant and returns a
// Slack slash-command reply payload. Query results are ephemeral (private to the
// caller); errors are reported plainly.
func chatopsExecute(ctx context.Context, ch *clickhouse.Client, pool *pgxpool.Pool, tenant string, cmd chatops.Command) []byte {
	switch cmd.Action {
	case chatops.ActionQuery:
		if strings.TrimSpace(cmd.Arg) == "" {
			return chatops.SlashReply("Usage: `/qeetlogs query <LogQL++>`", false)
		}
		compiled, err := query.Compile(cmd.Arg, tenant, queryOpts)
		if err != nil {
			return chatops.SlashReply("Query error: "+err.Error(), false)
		}
		rows, err := ch.Query(ctx, compiled.SQL)
		if err != nil {
			return chatops.SlashReply("Query execution failed: "+err.Error(), false)
		}
		return chatops.SlashReply(chatopsRenderRows(compiled.Columns, rows), false)

	case chatops.ActionIncidents:
		return chatops.SlashReply(chatopsRenderIncidents(ctx, pool, tenant), false)

	case chatops.ActionRCA:
		svc := strings.TrimSpace(cmd.Arg)
		if svc == "" {
			return chatops.SlashReply("Usage: `/qeetlogs rca <service>`", false)
		}
		return chatops.SlashReply(fmt.Sprintf("Root-cause analysis for *%s*: %s/incidents?service=%s",
			svc, chatopsConsoleURL(), url.QueryEscape(svc)), false)

	default:
		return chatops.SlashReply(chatops.HelpText(), false)
	}
}

// chatopsRenderRows formats up to 5 result rows as a compact Slack message.
func chatopsRenderRows(cols []string, rows []map[string]any) string {
	if len(rows) == 0 {
		return "No results."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "*%d row(s)* (showing up to 5):\n", len(rows))
	for i, row := range rows {
		if i == 5 {
			break
		}
		parts := make([]string, 0, len(cols))
		for _, c := range cols {
			if v, ok := row[c]; ok && v != nil {
				parts = append(parts, fmt.Sprintf("%s=%v", c, v))
			}
		}
		fmt.Fprintf(&b, "• %s\n", strings.Join(parts, "  "))
	}
	return b.String()
}

// chatopsRenderIncidents lists the tenant's open incidents (top 5).
func chatopsRenderIncidents(ctx context.Context, pool *pgxpool.Pool, tenant string) string {
	rows, err := pool.Query(ctx,
		`SELECT title, service, severity FROM incidents
		 WHERE tenant_id = $1::uuid AND status = 'open'
		 ORDER BY last_seen DESC LIMIT 5`, tenant)
	if err != nil {
		return "Could not fetch incidents: " + err.Error()
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("*Open incidents:*\n")
	n := 0
	for rows.Next() {
		var title, service, severity string
		if err := rows.Scan(&title, &service, &severity); err == nil {
			fmt.Fprintf(&b, "• [%s] %s (%s)\n", strings.ToUpper(severity), title, service)
			n++
		}
	}
	if n == 0 {
		return "No open incidents. 🎉"
	}
	return b.String()
}

// chatopsConsoleURL is the console base for deep links (env-overridable).
func chatopsConsoleURL() string {
	if u := os.Getenv("QEET_LOGS_CONSOLE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://logs.qeet.in"
}
