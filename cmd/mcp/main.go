// Command mcp is a stdio JSON-RPC 2.0 Model Context Protocol (MCP) server for
// Qeet Logs (PRD Module 29.3). It lets AI agents such as Claude Code and Cursor
// query logs, incidents, RCA, service topology, and deploy culprits.
//
// It speaks newline-delimited JSON-RPC 2.0 over stdin/stdout (no third-party
// deps) and proxies each tool call to the qeet-logs query API over HTTP:
//
//	base URL : QEET_LOGS_URL     (default http://localhost:8100)
//	auth     : X-Qeet-Api-Key header = QEET_LOGS_API_KEY
//
// Run it as a subprocess of an MCP-capable client:
//
//	go run ./cmd/mcp
//
// Example client config (Claude Code / Cursor):
//
//	{
//	  "mcpServers": {
//	    "qeet-logs": {
//	      "command": "qeet-logs-mcp",
//	      "env": { "QEET_LOGS_URL": "http://localhost:8100", "QEET_LOGS_API_KEY": "..." }
//	    }
//	  }
//	}
package main

import (
	"fmt"
	"os"
)

func main() {
	client := newQueryClient(os.Getenv("QEET_LOGS_URL"), os.Getenv("QEET_LOGS_API_KEY"))
	srv := newServer(client)
	if err := srv.run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "qeet-logs mcp: %v\n", err)
		os.Exit(1)
	}
}
