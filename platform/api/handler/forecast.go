package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/qeetgroup/qeet-logs/domains/forecast"
	"github.com/qeetgroup/qeet-logs/domains/query"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// forecastMaxBuckets bounds how many trailing time-buckets we pull for the fit
// (lookback = window * this). 288 * 5m ≈ 24h of history — enough for a daily
// season and a stable least-squares trend without an unbounded scan.
const forecastMaxBuckets = 288

// forecastDaySeconds is the seasonal cycle length used to derive the seasonal
// period (in windows) for the seasonal-naive baseline (14.2).
const forecastDaySeconds = 86400

// forecastEWMAAlpha is the smoothing factor for the reported EWMA level.
const forecastEWMAAlpha = 0.3

type forecastPoint struct {
	Bucket string  `json:"bucket"`
	Value  float64 `json:"value"`
}

type forecastResponse struct {
	Service       string          `json:"service"`
	Metric        string          `json:"metric"`
	WindowSeconds int64           `json:"window_seconds"`
	Horizon       int             `json:"horizon"`
	Points        int             `json:"points"`
	Series        []forecastPoint `json:"series"`

	// 14.1 capacity/exhaustion.
	Slope                  float64  `json:"slope"`     // per-window rate of change
	Intercept              float64  `json:"intercept"` // from the deploy-aware fit
	Current                float64  `json:"current"`   // last observed bucket value
	Threshold              *float64 `json:"threshold,omitempty"`
	ProjectedValue         float64  `json:"projected_value"`   // value `horizon` windows ahead
	TimeToThreshold        *float64 `json:"time_to_threshold"` // windows until breach; null = no breach / no threshold
	TimeToThresholdSeconds *float64 `json:"time_to_threshold_seconds"`
	WillBreach             bool     `json:"will_breach"`

	// 14.2 seasonal / deploy-aware trend.
	EWMA             float64  `json:"ewma"`                        // deploy-aware smoothed level
	SeasonalBaseline *float64 `json:"seasonal_baseline,omitempty"` // same-phase previous-season mean
	Deviation        *float64 `json:"deviation,omitempty"`         // signed deviation of current from baseline
	DeployResetIndex int      `json:"deploy_reset_index"`          // series index of the most recent deploy; -1 = full history

	Note string `json:"note,omitempty"`
}

// Forecast serves GET /v1/forecast — the statistical Predictive Observability
// tier (PRD Module 14.1 capacity/exhaustion + 14.2 seasonal/deploy-aware trend).
// It pulls the recent per-window series for (tenant, service, metric_name) from
// the metrics table (time-bucketed), fits a least-squares trend, projects when a
// threshold will breach, and reports an EWMA level + seasonal-naive baseline +
// deviation. The trend/level are DEPLOY-AWARE: history before the most recent
// deploy for the service is dropped so a behaviour shift doesn't poison the fit.
//
// Query params: ?service= (required) &metric= (required, metric_name)
// &window=<bucket seconds, default 300> &horizon=<windows ahead, default 12>
// &threshold=<float, optional>. Scopes: logs:read OR logs:query.
//
// This is the honest statistics-only tier; ONNX model tiers (PRD Module 14.3+)
// are Phase-3 and remain gated — no ML dependency is used here.
func Forecast(ch *clickhouse.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		service := r.URL.Query().Get("service")
		metric := r.URL.Query().Get("metric")
		if service == "" || metric == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service and metric are required"})
			return
		}

		window := int64(300)
		if s := r.URL.Query().Get("window"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v >= 10 {
				window = v
			}
		}
		horizon := 12
		if s := r.URL.Query().Get("horizon"); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 100000 {
				horizon = v
			}
		}
		var threshold *float64
		if s := r.URL.Query().Get("threshold"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				threshold = &v
			}
		}

		lookback := window * forecastMaxBuckets
		tq := query.QuoteLiteral(tenant)
		sq := query.QuoteLiteral(service)
		mq := query.QuoteLiteral(metric)

		sql := fmt.Sprintf(`SELECT toStartOfInterval(timestamp, INTERVAL %d SECOND) AS bucket,
			toUnixTimestamp(toStartOfInterval(timestamp, INTERVAL %d SECOND)) AS bts,
			avg(value) AS v
			FROM metrics
			WHERE tenant_id = %s AND service = %s AND metric_name = %s
			  AND timestamp > now() - INTERVAL %d SECOND
			GROUP BY bucket ORDER BY bucket ASC`,
			window, window, tq, sq, mq, lookback)
		rows, err := ch.Query(ctx, sql)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		series := make([]forecastPoint, 0, len(rows))
		vals := make([]float64, 0, len(rows))
		bts := make([]float64, 0, len(rows))
		for _, row := range rows {
			v := forecastNum(row["v"])
			series = append(series, forecastPoint{Bucket: forecastStr(row["bucket"]), Value: v})
			vals = append(vals, v)
			bts = append(bts, forecastNum(row["bts"]))
		}

		resp := forecastResponse{
			Service:          service,
			Metric:           metric,
			WindowSeconds:    window,
			Horizon:          horizon,
			Points:           len(vals),
			Series:           series,
			Threshold:        threshold,
			DeployResetIndex: -1,
		}

		if len(vals) < 2 {
			// Honest withholding: no trend from < 2 windows (mirrors the
			// anomaly scorer's insufficient-history contract).
			if len(vals) == 1 {
				resp.Current = vals[0]
				resp.ProjectedValue = vals[0]
				resp.Intercept = vals[0]
			}
			resp.Note = "insufficient history for a trend (need >= 2 windows)"
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// Deploy-aware baselining (14.2): drop history before the most recent
		// deploy so a step-change in behaviour doesn't poison the trend/level.
		resetIdx := forecastDeployReset(ctx, ch, tq, sq, lookback, bts)
		resp.DeployResetIndex = resetIdx

		sub := forecast.AfterReset(vals, resetIdx)
		slope, intercept := forecast.LinearForecast(sub)
		current := vals[len(vals)-1]
		resp.Slope = forecastRound(slope, 6)
		resp.Intercept = forecastRound(intercept, 6)
		resp.Current = current
		resp.EWMA = forecastRound(forecast.EWMA(sub, forecastEWMAAlpha), 6)
		resp.ProjectedValue = forecastRound(forecast.Project(len(sub), slope, intercept, horizon), 6)

		// Seasonal-naive baseline + deviation (14.2), if a daily season fits the
		// window and enough post-reset history exists.
		if period := int(forecastDaySeconds / window); period >= 2 {
			if base, ok := forecast.SeasonalBaseline(vals, period, resetIdx); ok {
				b := forecastRound(base, 6)
				d := forecastRound(forecast.Deviation(current, base), 4)
				resp.SeasonalBaseline = &b
				resp.Deviation = &d
			}
		}

		// Capacity / exhaustion projection (14.1).
		if threshold != nil {
			steps, breach := forecast.TimeToThreshold(current, slope, *threshold)
			resp.WillBreach = breach
			if breach && !math.IsInf(steps, 0) {
				stepsR := forecastRound(steps, 4)
				secs := forecastRound(steps*float64(window), 2)
				resp.TimeToThreshold = &stepsR
				resp.TimeToThresholdSeconds = &secs
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// forecastDeployReset returns the series index of the first bucket at/after the
// most recent 'deploy' change event for the service within the lookback, or -1
// if there is no deploy (or the lookup fails — best-effort). bts is the series'
// per-bucket unix timestamps, ascending.
func forecastDeployReset(ctx context.Context, ch *clickhouse.Client, tq, sq string, lookback int64, bts []float64) int {
	sql := fmt.Sprintf(`SELECT max(toUnixTimestamp(timestamp)) AS d FROM change_events
		WHERE tenant_id = %s AND service = %s AND kind = 'deploy'
		  AND timestamp > now() - INTERVAL %d SECOND`, tq, sq, lookback)
	rows, err := ch.Query(ctx, sql)
	if err != nil || len(rows) == 0 {
		return -1
	}
	d := forecastNum(rows[0]["d"])
	if d <= 0 {
		return -1
	}
	for i, t := range bts {
		if t >= d {
			if i == 0 {
				return -1 // deploy predates the whole series → full history is post-deploy
			}
			return i
		}
	}
	return -1 // deploy is more recent than the last bucket → no post-deploy data yet
}

func forecastNum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case string:
		var n float64
		fmt.Sscanf(x, "%g", &n)
		return n
	default:
		return 0
	}
}

func forecastStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func forecastRound(x float64, places int) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}
