package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/qeetgroup/qeet-logs/domains/query"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// Tail streams matching log records to a WebSocket in real time (PRD Module 05).
// The client supplies a TAIL LogQL++ statement via ?q=; records are read from
// the Redis tail.{tenant}.* channels the writer publishes to — no ClickHouse
// scan — and filtered in-process by the TAIL WHERE clause.
func Tail(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)

		stmt, err := query.Parse(r.URL.Query().Get("q"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if stmt.Kind != query.KindTail {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tail endpoint requires a TAIL statement"})
			return
		}
		if stmt.Table != "logs" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "live tail supports the logs table only"})
			return
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// Drive the subscription off a fresh context (not r.Context(), which
		// net/http may cancel after the WS hijack); CloseRead cancels connCtx
		// when the client disconnects.
		connCtx := c.CloseRead(context.Background())

		// Subscription is scoped to the authenticated tenant's channels only.
		// Subscribe on a background context (go-redis ties the message pump to
		// this context); close the subscription when the client disconnects.
		pattern := "tail." + tenant + ".*"
		pubsub := rdb.PSubscribe(context.Background(), pattern)
		defer pubsub.Close()
		go func() {
			<-connCtx.Done()
			pubsub.Close()
		}()
		msgs := pubsub.Channel()

		for {
			select {
			case <-connCtx.Done():
				return
			case m, ok := <-msgs:
				if !ok {
					return
				}
				var rec map[string]any
				if err := json.Unmarshal([]byte(m.Payload), &rec); err != nil {
					continue
				}
				if !query.Match(stmt.Where, rec) {
					continue
				}
				wctx, cancel := context.WithTimeout(connCtx, 5*time.Second)
				err := c.Write(wctx, websocket.MessageText, []byte(m.Payload))
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}
