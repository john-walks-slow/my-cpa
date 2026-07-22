package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
	"github.com/John/my-cpa/plugin/compare"
	"github.com/John/my-cpa/plugin/dashboard"
	"github.com/John/my-cpa/plugin/share"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type managementRegisterRequest struct {
	Plugin   pluginapi.Metadata `json:"plugin"`
	BasePath string             `json:"base_path"`
}
type managementHandleRequest struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Headers http.Header `json:"headers"`
	Query   url.Values  `json:"query"`
	Body    []byte      `json:"body"`
}

func (p *pluginState) handleManagementRegister(raw []byte) ([]byte, error) {
	p.mu.Lock()
	dashboardEnabled := p.cfg.IsDashboardEnabled()
	p.mu.Unlock()
	routes := []pluginapi.ManagementRoute{
		{Method: "GET", Path: "/stats/overview", Description: "Aggregate overview of all tracked metrics."},
		{Method: "GET", Path: "/stats/series", Description: "Time series query with window/model/auth filters."},
		{Method: "GET", Path: "/stats/by-model", Description: "Per-model aggregation for a given window."},
		{Method: "GET", Path: "/stats/by-auth", Description: "Per-auth aggregation for a given window."},
		{Method: "GET", Path: "/stats/keys", Description: "List known series keys."},
		{Method: "POST", Path: "/stats/reset", Description: "Reset all in-memory aggregation."},
		{Method: "GET", Path: "/stats/config", Description: "Current effective configuration."},
		{Method: "GET", Path: "/stats/compare", Description: "Build a comparison report from current metrics."},
		{Method: "POST", Path: "/stats/share", Description: "Create an immutable comparison snapshot."},
		{Method: "DELETE", Path: "/stats/share", Description: "Delete an immutable comparison snapshot."},
	}
	if dashboardEnabled {
		routes = append(routes, pluginapi.ManagementRoute{Method: "GET", Path: "/stats/insights", Description: "Server-side headline KPIs for the dashboard."})
	}
	resp := pluginapi.ManagementRegistrationResponse{Routes: routes}
	if dashboardEnabled {
		for _, sub := range dashboard.ResourcePaths() {
			menu := ""
			if sub == "/index.html" {
				menu = "Stats Dashboard"
			}
			resp.Resources = append(resp.Resources, pluginapi.ResourceRoute{Path: sub, Menu: menu, Description: "Stats dashboard asset."})
		}
	}
	return okEnvelope(resp)
}

func (p *pluginState) handleManagementHandle(raw []byte) ([]byte, error) {
	var req managementHandleRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Method == "GET" && req.Path == "/v0/resource/plugins/my-cpa-stats-plugin/share-data" {
		return okEnvelope(p.publicShare(req.Query, req.Headers))
	}
	if strings.HasPrefix(req.Path, "/v0/resource/plugins/my-cpa-stats-plugin/") {
		if resp, handled := dashboard.Serve(req.Path); handled {
			return okEnvelope(resp)
		}
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}))
	}
	path := strings.TrimPrefix(req.Path, "/v0/management")
	var resp pluginapi.ManagementResponse
	switch path {
	case "/stats/overview":
		resp = p.statsOverview()
	case "/stats/series":
		resp = p.statsSeries(req.Query)
	case "/stats/by-model":
		resp = p.statsByModel(req.Query)
	case "/stats/by-auth":
		resp = p.statsByAuth(req.Query)
	case "/stats/keys":
		resp = p.statsKeys()
	case "/stats/reset":
		resp = p.statsReset()
	case "/stats/config":
		resp = p.statsConfig()
	case "/stats/compare":
		resp = p.statsCompare(req.Query)
	case "/stats/share":
		resp = p.statsShare(req.Method, req.Query, req.Body, req.Headers)
	case "/stats/insights":
		resp = p.statsInsights()
	default:
		resp = jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return okEnvelope(resp)
}

type overviewResponse struct {
	TotalRequests  uint64  `json:"total_requests"`
	SuccessCount   uint64  `json:"success_count"`
	FailedCount    uint64  `json:"failed_count"`
	SuccessRate    float64 `json:"success_rate"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	AvgTTFTMs      float64 `json:"avg_ttft_ms"`
	SeriesCount    int     `json:"series_count"`
	DroppedSamples uint64  `json:"dropped_samples"`
}

func (p *pluginState) statsOverview() pluginapi.ManagementResponse {
	if p.agg == nil {
		return jsonResponse(http.StatusOK, overviewResponse{})
	}
	snap := p.agg.Snapshot()
	var total, failed uint64
	var sumLatency, sumTTFT time.Duration
	series := map[string]struct{}{}
	if m := snap[time.Minute]; m != nil {
		for k, b := range m {
			total += b.Count
			failed += b.Failed
			sumLatency += b.SumLatency
			sumTTFT += b.SumTTFT
			series[k] = struct{}{}
		}
	}
	resp := overviewResponse{TotalRequests: total, SuccessCount: total - failed, FailedCount: failed, SeriesCount: len(series), DroppedSamples: p.agg.DropCount()}
	if total > 0 {
		resp.SuccessRate = float64(total-failed) / float64(total)
		resp.AvgLatencyMs = float64(sumLatency.Milliseconds()) / float64(total)
		resp.AvgTTFTMs = float64(sumTTFT.Milliseconds()) / float64(total)
	}
	return jsonResponse(http.StatusOK, resp)
}

type seriesPoint struct {
	Key           string  `json:"key"`
	WindowStart   string  `json:"window_start"`
	Count         uint64  `json:"count"`
	Failed        uint64  `json:"failed"`
	SuccessRate   float64 `json:"success_rate"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	AvgTTFTMs     float64 `json:"avg_ttft_ms"`
	AvgStreamRate float64 `json:"avg_stream_rate_tps"`
	P50LatencyMs  float64 `json:"p50_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	SumInput      int64   `json:"sum_input_tokens"`
	SumOutput     int64   `json:"sum_output_tokens"`
	SumReasoning  int64   `json:"sum_reasoning_tokens"`
	SumCached     int64   `json:"sum_cached_tokens"`
}

func (p *pluginState) statsSeries(q url.Values) pluginapi.ManagementResponse {
	if p.agg == nil {
		return jsonResponse(http.StatusOK, []seriesPoint{})
	}
	w := parseWindow(q.Get("window"))
	modelFilter := q.Get("model")
	authFilter := q.Get("auth")
	limit := parseIntDefault(q.Get("limit"), 100)
	m := p.agg.Snapshot()[w]
	points := make([]seriesPoint, 0, len(m))
	for k, b := range m {
		if modelFilter != "" && !keyContainsModel(k, modelFilter) {
			continue
		}
		if authFilter != "" && !keyContainsAuth(k, authFilter) {
			continue
		}
		points = append(points, bucketToPoint(k, b))
	}
	sort.Slice(points, func(i, j int) bool { return points[i].WindowStart > points[j].WindowStart })
	if len(points) > limit {
		points = points[:limit]
	}
	return jsonResponse(http.StatusOK, points)
}
func (p *pluginState) statsByModel(q url.Values) pluginapi.ManagementResponse {
	if p.agg == nil {
		return jsonResponse(http.StatusOK, []seriesPoint{})
	}
	m := p.agg.Snapshot()[parseWindow(q.Get("window"))]
	grouped := map[string]*aggregator.Bucket{}
	for k, b := range m {
		parts := splitKey(k)
		key := parts[0] + "|" + parts[1]
		if existing := grouped[key]; existing != nil {
			mergeBuckets(existing, b)
		} else {
			cp := *b
			grouped[key] = &cp
		}
	}
	points := make([]seriesPoint, 0, len(grouped))
	for k, b := range grouped {
		points = append(points, bucketToPoint(k, b))
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Count > points[j].Count })
	return jsonResponse(http.StatusOK, points)
}
func (p *pluginState) statsByAuth(q url.Values) pluginapi.ManagementResponse {
	if p.agg == nil {
		return jsonResponse(http.StatusOK, []seriesPoint{})
	}
	m := p.agg.Snapshot()[parseWindow(q.Get("window"))]
	modelFilter := q.Get("model")
	grouped := map[string]*aggregator.Bucket{}
	for k, b := range m {
		parts := splitKey(k)
		if modelFilter != "" && parts[1] != modelFilter && parts[2] != modelFilter {
			continue
		}
		key := parts[0] + "|" + parts[3]
		if existing := grouped[key]; existing != nil {
			mergeBuckets(existing, b)
		} else {
			cp := *b
			grouped[key] = &cp
		}
	}
	points := make([]seriesPoint, 0, len(grouped))
	for k, b := range grouped {
		points = append(points, bucketToPoint(k, b))
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Count > points[j].Count })
	return jsonResponse(http.StatusOK, points)
}
func (p *pluginState) statsKeys() pluginapi.ManagementResponse {
	if p.agg == nil {
		return jsonResponse(http.StatusOK, []string{})
	}
	keys := []string{}
	for k := range p.agg.Snapshot()[time.Minute] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return jsonResponse(http.StatusOK, keys)
}
func (p *pluginState) statsReset() pluginapi.ManagementResponse {
	if p.agg != nil {
		p.agg.Reset()
	}
	return jsonResponse(http.StatusOK, map[string]string{"status": "reset"})
}
func (p *pluginState) statsConfig() pluginapi.ManagementResponse {
	p.mu.Lock()
	cfg := p.cfg
	p.mu.Unlock()
	return jsonResponse(http.StatusOK, map[string]any{"enabled": cfg.Enabled, "retention_minutes": cfg.RetentionMinutes, "persist_path": cfg.PersistPath, "persist_interval_sec": cfg.PersistIntervalSec, "cardinality_limit": cfg.CardinalityLimit, "dashboard_enabled": cfg.IsDashboardEnabled(), "share_enabled": cfg.ShareEnabled, "share_path": cfg.SharePath, "share_max_count": cfg.ShareMaxCount, "share_cleanup_interval_sec": cfg.ShareCleanupSec, "share_max_snapshot_bytes": cfg.ShareMaxSnapshot, "share_available": p.shares != nil})
}

const insightsCacheTTL = 60 * time.Second

func (p *pluginState) statsInsights() pluginapi.ManagementResponse {
	p.insightsMu.Lock()
	defer p.insightsMu.Unlock()
	if p.insightsCache != nil && time.Since(p.insightsAt) < insightsCacheTTL {
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		return pluginapi.ManagementResponse{StatusCode: 200, Headers: h, Body: p.insightsCache}
	}
	if p.agg == nil {
		return jsonResponse(http.StatusOK, dashboard.InsightsResponse{GeneratedAt: time.Now().Format(time.RFC3339)})
	}
	body, _ := json.Marshal(dashboard.ComputeInsights(p.agg.Snapshot(), time.Now()))
	p.insightsCache = body
	p.insightsAt = time.Now()
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return pluginapi.ManagementResponse{StatusCode: 200, Headers: h, Body: body}
}
func jsonResponse(status int, v any) pluginapi.ManagementResponse {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"response encoding failed"}`)
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return pluginapi.ManagementResponse{StatusCode: status, Headers: h, Body: body}
}
func bucketToPoint(key string, b *aggregator.Bucket) seriesPoint {
	return seriesPoint{Key: key, WindowStart: b.Start.Format(time.RFC3339), Count: b.Count, Failed: b.Failed, SuccessRate: b.SuccessRate(), AvgLatencyMs: float64(b.AvgLatency().Milliseconds()), AvgTTFTMs: float64(b.AvgTTFT().Milliseconds()), AvgStreamRate: b.AvgStreamRate(), P50LatencyMs: float64(b.Percentile(.5).Milliseconds()), P95LatencyMs: float64(b.Percentile(.95).Milliseconds()), SumInput: b.SumInput, SumOutput: b.SumOutput, SumReasoning: b.SumReasoning, SumCached: b.SumCached}
}
func mergeBuckets(dst, src *aggregator.Bucket) {
	dst.Count += src.Count
	dst.Failed += src.Failed
	dst.SumLatency += src.SumLatency
	dst.SumTTFT += src.SumTTFT
	dst.SumOutput += src.SumOutput
	dst.SumInput += src.SumInput
	dst.SumReasoning += src.SumReasoning
	dst.SumCached += src.SumCached
	dst.StreamRateSum += src.StreamRateSum
	dst.StreamRateCount += src.StreamRateCount
	if src.LastSampleAt.After(dst.LastSampleAt) {
		dst.LastSampleAt = src.LastSampleAt
	}
}
func parseWindow(s string) time.Duration {
	switch s {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return time.Minute
	}
}
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}
func splitKey(key string) [4]string { return aggregator.SplitSeriesKey(key) }
func keyContainsModel(key, model string) bool {
	parts := splitKey(key)
	return parts[1] == model || parts[2] == model
}
func keyContainsAuth(key, auth string) bool { return splitKey(key)[3] == auth }

type shareRequest struct {
	Kind         string   `json:"kind"`
	IDs          []string `json:"ids"`
	Range        string   `json:"range"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Metric       string   `json:"metric"`
	RequireToken bool     `json:"require_token"`
	ExpiresIn    string   `json:"expires_in"`
}

func parseCompareRequest(q url.Values) compare.Request {
	ids := append([]string{}, q["id"]...)
	if len(ids) == 0 {
		ids = append(ids, q["ids"]...)
	}
	var expanded []string
	for _, v := range ids {
		for _, id := range strings.Split(v, ",") {
			if strings.TrimSpace(id) != "" {
				expanded = append(expanded, strings.TrimSpace(id))
			}
		}
	}
	return compare.Request{Kind: q.Get("kind"), IDs: expanded, Range: q.Get("range"), From: q.Get("from"), To: q.Get("to"), Metric: q.Get("metric")}
}
func (p *pluginState) statsCompare(q url.Values) pluginapi.ManagementResponse {
	p.mu.Lock()
	agg, retention := p.agg, p.cfg.Retention()
	p.mu.Unlock()
	if agg == nil {
		return jsonResponse(http.StatusOK, compare.Report{SchemaVersion: 1, Rows: []compare.Row{}})
	}
	report, err := compare.BuildTimeline(agg.Snapshot(), map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: agg.Timeline(time.Minute), 5 * time.Minute: agg.Timeline(5 * time.Minute), 15 * time.Minute: agg.Timeline(15 * time.Minute), time.Hour: agg.Timeline(time.Hour),
	}, parseCompareRequest(q), retention, time.Now())
	if err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, report)
}
func (p *pluginState) statsShare(method string, q url.Values, body []byte, _ http.Header) pluginapi.ManagementResponse {
	if method == "DELETE" {
		if p.shares == nil || p.shares.Delete(q.Get("id")) != nil {
			return jsonResponse(404, map[string]string{"error": "not found"})
		}
		return jsonResponse(200, map[string]string{"status": "deleted"})
	}
	if method != "POST" {
		return jsonResponse(405, map[string]string{"error": "method not allowed"})
	}
	if p.shares == nil {
		return jsonResponse(503, map[string]string{"error": "sharing is not configured"})
	}
	var req shareRequest
	if json.Unmarshal(body, &req) != nil {
		return jsonResponse(400, map[string]string{"error": "invalid JSON body"})
	}
	p.mu.Lock()
	agg, retention := p.agg, p.cfg.Retention()
	p.mu.Unlock()
	if agg == nil {
		return jsonResponse(503, map[string]string{"error": "aggregator is not ready"})
	}
	report, err := compare.BuildTimeline(agg.Snapshot(), map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: agg.Timeline(time.Minute), 5 * time.Minute: agg.Timeline(5 * time.Minute), 15 * time.Minute: agg.Timeline(15 * time.Minute), time.Hour: agg.Timeline(time.Hour),
	}, compare.Request{Kind: req.Kind, IDs: req.IDs, Range: req.Range, From: req.From, To: req.To, Metric: req.Metric}, retention, time.Now())
	if err != nil {
		return jsonResponse(400, map[string]string{"error": err.Error()})
	}
	created := time.Now().UTC()
	var expires *time.Time
	switch req.ExpiresIn {
	case "24h":
		t := created.Add(24 * time.Hour)
		expires = &t
	case "7d":
		t := created.Add(7 * 24 * time.Hour)
		expires = &t
	case "30d":
		t := created.Add(30 * 24 * time.Hour)
		expires = &t
	case "", "never":
	default:
		return jsonResponse(400, map[string]string{"error": "invalid expires_in"})
	}
	token, hash := "", ""
	if req.RequireToken {
		token, hash, err = share.NewToken()
		if err != nil {
			return jsonResponse(500, map[string]string{"error": "token generation failed"})
		}
	}
	id, err := p.shares.Create(share.Snapshot{CreatedAt: created, ExpiresAt: expires, RequireToken: req.RequireToken, TokenSHA256: hash, Title: report.Title, Range: report.Range, Metric: report.Metric, Subjects: report.Subjects, Rows: report.Rows})
	if err != nil {
		return jsonResponse(500, map[string]string{"error": err.Error()})
	}
	short := "/v0/resource/plugins/my-cpa-stats-plugin/share.html?id=" + url.QueryEscape(id)
	result := map[string]any{"id": id, "short_url": short, "expires_at": expires}
	if token != "" {
		result["url"] = short + "&token=" + url.QueryEscape(token)
	}
	return jsonResponse(201, result)
}
func (p *pluginState) publicShare(q url.Values, headers http.Header) pluginapi.ManagementResponse {
	if p.shares == nil {
		return jsonResponse(404, map[string]string{"error": "not found"})
	}
	token := q.Get("token")
	if token == "" {
		token = cookieValue(headers.Get("Cookie"), "cpa_share_token")
	}
	snapshot, err := p.shares.Read(q.Get("id"), token)
	if err != nil {
		return jsonResponse(404, map[string]string{"error": "not found"})
	}
	resp := jsonResponse(200, snapshot)
	if snapshot.RequireToken && q.Get("token") != "" {
		resp.Headers.Set("Set-Cookie", shareCookie(token, headers))
	}
	return resp
}

// shareCookie builds the Set-Cookie header for the share token.
// Secure flag is set when the request arrived via HTTPS or when an upstream proxy
// indicates TLS via X-Forwarded-Proto. Hosts that terminate TLS outside the proxy
// must forward that header for cookies to be marked Secure.
func shareCookie(token string, headers http.Header) string {
	secure := isTLSRequest(headers)
	parts := []string{
		"cpa_share_token=" + url.QueryEscape(token),
		"Path=/v0/resource/plugins/my-cpa-stats-plugin",
		"HttpOnly",
		"SameSite=Lax",
	}
	if secure {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

func isTLSRequest(headers http.Header) bool {
	if proto := strings.ToLower(strings.TrimSpace(headers.Get("X-Forwarded-Proto"))); proto == "https" {
		return true
	}
	if strings.EqualFold(headers.Get("X-Forwarded-Ssl"), "on") {
		return true
	}
	if strings.EqualFold(headers.Get("Front-End-Https"), "on") {
		return true
	}
	return false
}
func cookieValue(raw, name string) string {
	for _, part := range strings.Split(raw, ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 && pair[0] == name {
			return pair[1]
		}
	}
	return ""
}
