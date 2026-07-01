// Package alerting implements the qeet-logs threshold + absence alerting engine
// (Module 07). The Engine polls alert_rules from Postgres, evaluates each rule
// against ClickHouse log counts, tracks firing state in alert_state, and
// delivers notifications via configured channels on state transitions.
package alerting

import (
	"encoding/json"
	"time"
)

// Kind identifies the alert evaluation strategy.
const (
	KindThreshold = "threshold" // fires when count() > threshold within window
	KindAbsence   = "absence"   // fires when count() == 0 within window
)

// Channel is one delivery target within an alert rule.
type Channel struct {
	Type   string `json:"type"`   // "webhook" | "qeet_notify"
	Target string `json:"target"` // URL for webhook; recipient address for notify
}

// AlertRule mirrors the alert_rules Postgres table row.
type AlertRule struct {
	ID            string    `db:"id"`
	TenantID      string    `db:"tenant_id"`
	Name          string    `db:"name"`
	Kind          string    `db:"kind"`
	Service       *string   `db:"service"`
	Condition     *string   `db:"condition"`
	Threshold     *float64  `db:"threshold"`
	WindowSeconds int       `db:"window_seconds"`
	Channels      []Channel // decoded from JSONB
	Enabled       bool      `db:"enabled"`
	CreatedAt     time.Time `db:"created_at"`
}

// AlertState is the persisted firing state for one rule.
type AlertState struct {
	RuleID     string
	TenantID   string
	Firing     bool
	FiredAt    *time.Time
	ResolvedAt *time.Time
	LastEval   time.Time
}

// Payload is the JSON body delivered to webhook channels on state changes.
type Payload struct {
	AlertID   string    `json:"alert_id"`
	AlertName string    `json:"alert_name"`
	TenantID  string    `json:"tenant_id"`
	Kind      string    `json:"kind"`
	Firing    bool      `json:"firing"`
	Count     int64     `json:"count,omitempty"`
	FiredAt   time.Time `json:"fired_at,omitempty"`
	Message   string    `json:"message"`
}

// decodeChannels deserialises the JSONB channels column.
func decodeChannels(raw []byte) ([]Channel, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var ch []Channel
	return ch, json.Unmarshal(raw, &ch)
}
