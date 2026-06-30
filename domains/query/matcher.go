package query

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Match reports whether a decoded log record (as published to the live-tail
// Redis channel) satisfies a WHERE expression. A nil expression matches all.
//
// This mirrors the SQL compiler's semantics closely enough for live filtering:
// string/number compares, severity-aware level ordering, time vs now()±dur, and
// body.x / resource.x extraction. Tenant predicates are always satisfied (the
// subscription is already scoped to the authenticated tenant's channel).
func Match(e Expr, rec map[string]any) bool {
	if e == nil {
		return true
	}
	return eval(e, rec)
}

func eval(e Expr, rec map[string]any) bool {
	switch v := e.(type) {
	case Paren:
		return eval(v.Inner, rec)
	case Binary:
		if strings.EqualFold(v.Op, "AND") {
			return eval(v.Left, rec) && eval(v.Right, rec)
		}
		return eval(v.Left, rec) || eval(v.Right, rec)
	case Comparison:
		return evalComparison(v, rec)
	default:
		return false
	}
}

func evalComparison(c Comparison, rec map[string]any) bool {
	if len(c.Field.Parts) == 1 {
		switch strings.ToLower(c.Field.Parts[0]) {
		case "tenant", "tenant_id":
			return true // already scoped to the authenticated tenant
		}
	}
	lhs, ok := fieldString(c.Field, rec)
	if !ok {
		return false
	}

	// Severity-aware level ordering.
	if len(c.Field.Parts) == 1 && strings.EqualFold(c.Field.Parts[0], "level") &&
		c.Value.Kind == ValString && isOrdering(c.Op) {
		return applyOp(c.Op, levelRank(lhs)-levelRank(c.Value.Str))
	}

	switch c.Value.Kind {
	case ValTime:
		lt, err := parseTime(lhs)
		if err != nil {
			return false
		}
		base := time.Now()
		off := time.Duration(c.Value.TimeSeconds) * time.Second
		if c.Value.TimeSub {
			base = base.Add(-off)
		} else {
			base = base.Add(off)
		}
		return applyOp(c.Op, lt.Compare(base))
	case ValNumber:
		ln, err1 := strconv.ParseFloat(lhs, 64)
		rn, err2 := strconv.ParseFloat(c.Value.Num, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		switch {
		case ln < rn:
			return applyOp(c.Op, -1)
		case ln > rn:
			return applyOp(c.Op, 1)
		default:
			return applyOp(c.Op, 0)
		}
	default: // ValString
		return applyOp(c.Op, strings.Compare(lhs, c.Value.Str))
	}
}

// fieldString resolves a field to its string value in the record.
func fieldString(f Field, rec map[string]any) (string, bool) {
	if len(f.Parts) == 1 {
		name := strings.ToLower(f.Parts[0])
		switch name {
		case "time", "timestamp":
			name = "timestamp"
		case "tenant":
			name = "tenant_id"
		}
		v, ok := rec[name]
		if !ok {
			return "", false
		}
		return stringify(v), true
	}
	switch strings.ToLower(f.Parts[0]) {
	case "body":
		return extractJSON(rec["body"], f.Parts[1:])
	case "resource":
		return extractJSON(rec["resource"], f.Parts[1:])
	case "auth":
		if len(f.Parts) == 2 {
			if v, ok := rec[strings.ToLower(f.Parts[1])]; ok {
				return stringify(v), true
			}
		}
	}
	return "", false
}

func extractJSON(raw any, path []string) (string, bool) {
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", false
	}
	var cur any
	if err := json.Unmarshal([]byte(s), &cur); err != nil {
		return "", false
	}
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[key]
		if !ok {
			return "", false
		}
	}
	return stringify(cur), true
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func levelRank(level string) int {
	for i, l := range []string{"trace", "debug", "info", "warn", "error", "fatal"} {
		if strings.EqualFold(level, l) {
			return i
		}
	}
	return -1
}

// applyOp turns a three-way comparison result into a boolean for the operator.
func applyOp(op string, cmp int) bool {
	switch op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case ">=":
		return cmp >= 0
	}
	return false
}
