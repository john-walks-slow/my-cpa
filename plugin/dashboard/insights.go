package dashboard

import (
	"fmt"
	"sort"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
)

type InsightsResponse struct {
	FastestModel    InsightRow `json:"fastest_model"`
	SlowestModel    InsightRow `json:"slowest_model"`
	MostStableModel InsightRow `json:"most_stable_model"`
	LastAnomaly     InsightRow `json:"last_anomaly"`
	GeneratedAt     string     `json:"generated_at"`
}

type InsightRow struct {
	Model  string  `json:"model"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Detail string  `json:"detail"`
}

type modelAcc struct {
	sumLatMs float64
	sumP95Ms float64
	count    uint64
	failed   uint64
	lastSeen time.Time
	auths    map[string]struct{}
}

// ComputeInsights scans the 1-minute window of a snapshot and derives four
// headline KPIs. Pure function — no I/O, no clock except GeneratedAt.
func ComputeInsights(snapshot map[time.Duration]map[string]*aggregator.Bucket, now time.Time) InsightsResponse {
	resp := InsightsResponse{GeneratedAt: now.Format(time.RFC3339)}

	m1 := snapshot[time.Minute]
	if len(m1) == 0 {
		return resp
	}

	accs := make(map[string]*modelAcc)
	for k, b := range m1 {
		parts := aggregator.SplitSeriesKey(k)
		model := parts[1]
		if model == "" {
			continue
		}
		a := accs[model]
		if a == nil {
			a = &modelAcc{auths: make(map[string]struct{})}
			accs[model] = a
		}
		w := float64(b.Count)
		a.sumLatMs += float64(b.AvgLatency().Milliseconds()) * w
		a.sumP95Ms += float64(b.Percentile(0.95).Milliseconds()) * w
		a.count += b.Count
		a.failed += b.Failed
		if b.LastSampleAt.After(a.lastSeen) {
			a.lastSeen = b.LastSampleAt
		}
		a.auths[parts[3]] = struct{}{}
	}

	type ranked struct {
		model string
		acc   *modelAcc
		p50   float64
		p95   float64
	}
	rows := make([]ranked, 0, len(accs))
	for model, a := range accs {
		if a.count == 0 {
			continue
		}
		rows = append(rows, ranked{
			model: model,
			acc:   a,
			p50:   a.sumLatMs / float64(a.count),
			p95:   a.sumP95Ms / float64(a.count),
		})
	}
	if len(rows) == 0 {
		return resp
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].p50 < rows[j].p50 })
	fastest, slowest := rows[0], rows[len(rows)-1]
	resp.FastestModel = InsightRow{
		Model:  fastest.model,
		Value:  fastest.p50,
		Unit:   "ms",
		Detail: fmt.Sprintf("P50 latency across %d auths", len(fastest.acc.auths)),
	}
	resp.SlowestModel = InsightRow{
		Model:  slowest.model,
		Value:  slowest.p50,
		Unit:   "ms",
		Detail: fmt.Sprintf("P50 latency across %d auths", len(slowest.acc.auths)),
	}

	// Most stable = lowest P95/P50 ratio (requires p50 > 0).
	var stable *ranked
	for i := range rows {
		if rows[i].p50 <= 0 {
			continue
		}
		if stable == nil || rows[i].p95/rows[i].p50 < stable.p95/stable.p50 {
			stable = &rows[i]
		}
	}
	if stable != nil {
		resp.MostStableModel = InsightRow{
			Model:  stable.model,
			Value:  stable.p95 / stable.p50,
			Unit:   "x",
			Detail: fmt.Sprintf("P95/P50 ratio across %d auths", len(stable.acc.auths)),
		}
	}

	// Last anomaly = model with highest recent fail rate (tie-break: most recent).
	var anomaly *ranked
	var anomalyRate float64
	for i := range rows {
		rate := float64(rows[i].acc.failed) / float64(rows[i].acc.count)
		if rate <= 0 {
			continue
		}
		if anomaly == nil || rate > anomalyRate ||
			(rate == anomalyRate && rows[i].acc.lastSeen.After(anomaly.acc.lastSeen)) {
			anomaly = &rows[i]
			anomalyRate = rate
		}
	}
	if anomaly != nil {
		resp.LastAnomaly = InsightRow{
			Model:  anomaly.model,
			Value:  anomalyRate * 100,
			Unit:   "%",
			Detail: fmt.Sprintf("error rate, last seen %s", relTime(anomaly.acc.lastSeen, now)),
		}
	}

	return resp
}

func relTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
