// Command ql is the Qeet Logs CLI (PRD Module 29.2): tail logs, run queries,
// send events (CI ingestion checks), and inspect incidents/topology/RCA from a
// terminal or pipeline. Zero third-party dependencies (net/http + flag).
//
// Config (env, overridable by flags):
//
//	QEET_LOGS_API_KEY     required — X-Qeet-Api-Key
//	QEET_LOGS_URL         query API base   (default http://localhost:8100)
//	QEET_LOGS_INGEST_URL  ingest gateway   (default http://localhost:8101)
//
// Usage:
//
//	ql query "SELECT service, level, message FROM logs WHERE level='error'"
//	ql send --service ci --level info --message "deploy smoke ok"
//	ql tail --service payments
//	ql incidents [--status open]
//	ql topology [--service payments]
//	ql rca --service payments
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "query":
		err = cmdQuery(args)
	case "send":
		err = cmdSend(args)
	case "tail":
		err = cmdTail(args)
	case "incidents":
		err = cmdGet("/v1/incidents", args, "status")
	case "topology":
		err = cmdGet("/v1/topology", args, "service")
	case "rca":
		err = cmdGet("/v1/rca", args, "service")
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `qeet logs CLI

  ql query "<LogQL++>"                 run a query
  ql send --service S --message M      ingest one event (CI checks)
  ql tail --service S                  live-poll recent logs
  ql incidents [--status open]         list correlated incidents
  ql topology [--service S]            service dependency graph
  ql rca --service S                   structural root-cause candidates

env: QEET_LOGS_API_KEY, QEET_LOGS_URL, QEET_LOGS_INGEST_URL
`)
}

// ── subcommands ───────────────────────────────────────────────────────────────

func cmdQuery(args []string) error {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	format := fs.String("format", "json", "output format: json|ndjson")
	_ = fs.Parse(args)
	q := fs.Arg(0)
	if q == "" {
		return fmt.Errorf("usage: ql query \"<LogQL++>\"")
	}
	body, err := doGet(queryBase()+"/v1/query", url.Values{"q": {q}, "format": {*format}})
	if err != nil {
		return err
	}
	os.Stdout.Write(body)
	fmt.Println()
	return nil
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	service := fs.String("service", "", "service name (required)")
	level := fs.String("level", "info", "level")
	message := fs.String("message", "", "message (required)")
	trace := fs.String("trace", "", "trace_id")
	_ = fs.Parse(args)
	if *service == "" || *message == "" {
		return fmt.Errorf("usage: ql send --service S --message M [--level L] [--trace T]")
	}
	rec := map[string]any{"service": *service, "level": *level, "message": *message}
	if *trace != "" {
		rec["trace_id"] = *trace
	}
	b, _ := json.Marshal(rec)
	body, err := doPost(ingestBase()+"/v1/ingest", "application/json", b)
	if err != nil {
		return err
	}
	os.Stdout.Write(body)
	fmt.Println()
	return nil
}

func cmdTail(args []string) error {
	fs := flag.NewFlagSet("tail", flag.ExitOnError)
	service := fs.String("service", "", "service to tail (required)")
	interval := fs.Duration("interval", 2*time.Second, "poll interval")
	_ = fs.Parse(args)
	if *service == "" {
		return fmt.Errorf("usage: ql tail --service S")
	}
	q := fmt.Sprintf("SELECT id, timestamp, level, message FROM logs WHERE service = '%s' ORDER BY timestamp DESC LIMIT 20",
		escapeSingle(*service))
	seen := map[string]bool{}
	for {
		body, err := doGet(queryBase()+"/v1/query", url.Values{"q": {q}})
		if err != nil {
			return err
		}
		var res struct {
			Rows []map[string]any `json:"rows"`
		}
		if err := json.Unmarshal(body, &res); err == nil {
			// Print oldest-first among the new rows.
			for i := len(res.Rows) - 1; i >= 0; i-- {
				row := res.Rows[i]
				id, _ := row["id"].(string)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				fmt.Printf("%v [%v] %v\n", row["timestamp"], row["level"], row["message"])
			}
		}
		time.Sleep(*interval)
	}
}

// cmdGet runs a simple GET and pretty-prints JSON, forwarding one optional flag.
func cmdGet(path string, args []string, flagName string) error {
	fs := flag.NewFlagSet(path, flag.ExitOnError)
	val := fs.String(flagName, "", flagName+" filter")
	since := fs.String("since", "", "window seconds")
	_ = fs.Parse(args)
	q := url.Values{}
	if *val != "" {
		q.Set(flagName, *val)
	}
	if *since != "" {
		q.Set("since", *since)
	}
	body, err := doGet(queryBase()+path, q)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		os.Stdout.Write(pretty.Bytes())
	} else {
		os.Stdout.Write(body)
	}
	fmt.Println()
	return nil
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

var client = &http.Client{Timeout: 30 * time.Second}

func doGet(base string, q url.Values) ([]byte, error) {
	u := base
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	return send(req)
}

func doPost(u, contentType string, body []byte) ([]byte, error) {
	req, _ := http.NewRequest("POST", u, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	return send(req)
}

func send(req *http.Request) ([]byte, error) {
	key := os.Getenv("QEET_LOGS_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("QEET_LOGS_API_KEY is not set")
	}
	req.Header.Set("X-Qeet-Api-Key", key)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func queryBase() string  { return envOr("QEET_LOGS_URL", "http://localhost:8100") }
func ingestBase() string { return envOr("QEET_LOGS_INGEST_URL", "http://localhost:8101") }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func escapeSingle(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
