package compare

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
	"github.com/John/my-cpa/plugin/persist"
)

func TestValidateRequest(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a"}, Range: "1h"}, 24*time.Hour, now); err == nil {
		t.Fatal("expected subject count error")
	}
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "7d"}, 24*time.Hour, now); err == nil {
		t.Fatal("expected retention error")
	}
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "1h", Metric: "bad"}, 24*time.Hour, now); err == nil {
		t.Fatal("expected metric error")
	}
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{" a ", "a"}, Range: "1h"}, 24*time.Hour, now); err == nil {
		t.Fatal("expected duplicate trim+dedup error")
	}
}

func TestBuildReportByModelAndTrend(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	for _, sample := range []aggregator.Sample{
		{Provider: "openai", Model: "fast", AuthID: "one", RequestedAt: now.Add(-50 * time.Minute), Latency: 100 * time.Millisecond, TTFT: 20 * time.Millisecond, OutputTokens: 100},
		{Provider: "openai", Model: "fast", AuthID: "one", RequestedAt: now.Add(-10 * time.Minute), Latency: 120 * time.Millisecond, TTFT: 30 * time.Millisecond, OutputTokens: 100},
		{Provider: "openai", Model: "slow", AuthID: "two", RequestedAt: now.Add(-10 * time.Minute), Latency: 300 * time.Millisecond, TTFT: 50 * time.Millisecond, OutputTokens: 100},
	} {
		a.IngestDirect(sample)
	}
	report, err := Build(a.Snapshot(), Request{Kind: KindModel, IDs: []string{"fast", "slow"}, Range: "1h", Metric: MetricAvgLatency}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 || report.Rows[0].Count != 2 || len(report.Rows[0].Series) != 1 {
		t.Fatalf("unexpected report: %+v", report.Rows)
	}
	if report.Rows[1].AvgLatencyMs < 299 || report.Rows[1].AvgLatencyMs > 301 {
		t.Errorf("latency = %v", report.Rows[1].AvgLatencyMs)
	}
}

func TestBuildReportByAuth(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	a.IngestDirect(aggregator.Sample{Provider: "p", Model: "m", AuthID: "auth-a", RequestedAt: now.Add(-5 * time.Minute), Latency: time.Second})
	report, err := Build(a.Snapshot(), Request{Kind: KindAuth, IDs: []string{"auth-a", "auth-b"}, Range: "1h"}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows[0].Count != 1 || report.Rows[1].Count != 0 {
		t.Fatalf("unexpected auth rows: %+v", report.Rows)
	}
}

// TestTTP95CrossBucket covers B-12 fix: cross-bucket TTFT P95 must aggregate
// samples from every contributing bucket's reservoir and compute the percentile
// over the union. Without this, a 10-bucket scenario with each P95=100ms reported
// 0.1ms (sum/count). Correct result is ~100ms.
func TestTTP95CrossBucket(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	// Spread 10 minute-buckets with P95 around 100ms.
	for i := 0; i < 10; i++ {
		at := now.Add(-time.Duration(5+i) * time.Minute)
		for j := 0; j < 100; j++ {
			a.IngestDirect(aggregator.Sample{
				Provider:     "openai",
				Model:        "fast",
				AuthID:       "one",
				RequestedAt:  at.Add(time.Duration(j) * time.Millisecond),
				Latency:      500 * time.Millisecond,
				TTFT:         100 * time.Millisecond,
				OutputTokens: 50,
			})
		}
	}
	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: a.Timeline(time.Minute),
	}
	report, err := BuildTimeline(a.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{"fast", "slow"}, Range: "1h", Metric: MetricP95TTFT}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	var fastRow *Row
	for i := range report.Rows {
		if report.Rows[i].Subject == "fast" {
			fastRow = &report.Rows[i]
			break
		}
	}
	if fastRow == nil {
		t.Fatalf("fast subject missing: %+v", report.Rows)
	}
	p95 := fastRow.P95TTFTMs
	if p95 < 95 || p95 > 105 {
		t.Errorf("TTFT P95 = %v ms, want ~100ms (B-12 regression check)", p95)
	}
}

// TestRangeBounds covers S-06: 7d range rejected against 24h retention; custom
// range above retention rejected; custom range equal to retention accepted.
func TestRangeBounds(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "7d"}, 24*time.Hour, now); err == nil {
		t.Fatal("expected 7d range to exceed 24h retention")
	}
	from := now.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	to := now.UTC().Format(time.RFC3339)
	if _, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "custom", From: from, To: to}, 24*time.Hour, now); err == nil {
		t.Fatal("expected custom range above retention to fail")
	}
	from = now.Add(-12 * time.Hour).UTC().Format(time.RFC3339)
	if rng, err := ValidateRequest(Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "custom", From: from, To: to}, 24*time.Hour, now); err != nil {
		t.Fatalf("valid custom range rejected: %v", err)
	} else if rng.Preset != "custom" {
		t.Errorf("preset = %q, want custom", rng.Preset)
	}
}

// TestTrendNaNGuard covers B-04: zero historical denominator must not produce
// NaN/Inf in the trend field.
func TestTrendNaNGuard(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	// Only current data, no previous window samples.
	a.IngestDirect(aggregator.Sample{Provider: "p", Model: "m", AuthID: "x", RequestedAt: now.Add(-5 * time.Minute), Latency: 100 * time.Millisecond, TTFT: 10 * time.Millisecond})
	a.IngestDirect(aggregator.Sample{Provider: "p", Model: "n", AuthID: "x", RequestedAt: now.Add(-5 * time.Minute), Latency: 200 * time.Millisecond, TTFT: 20 * time.Millisecond})
	report, err := Build(a.Snapshot(), Request{Kind: KindModel, IDs: []string{"m", "n"}, Range: "1h", Metric: MetricAvgLatency}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range report.Rows {
		if row.Trend24hOver24h != 0 {
			t.Errorf("trend = %v, want 0 (no NaN/Inf)", row.Trend24hOver24h)
		}
	}
}

// TestMergeMetricsAcrossSubjects covers B-03: multiple provider/auth entries for
// the same subject must be combined on raw counts, not on ratios.
func TestMergeMetricsAcrossSubjects(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	a.IngestDirect(aggregator.Sample{Provider: "p1", Model: "m", AuthID: "auth-a", RequestedAt: now.Add(-10 * time.Minute), Latency: 100 * time.Millisecond})
	a.IngestDirect(aggregator.Sample{Provider: "p1", Model: "m", AuthID: "auth-a", RequestedAt: now.Add(-9 * time.Minute), Latency: 300 * time.Millisecond, Failed: true})
	a.IngestDirect(aggregator.Sample{Provider: "p2", Model: "n", AuthID: "auth-b", RequestedAt: now.Add(-8 * time.Minute), Latency: 500 * time.Millisecond})
	report, err := Build(a.Snapshot(), Request{Kind: KindModel, IDs: []string{"m", "n"}, Range: "1h", Metric: MetricAvgLatency}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	var m, n *Row
	for i := range report.Rows {
		switch report.Rows[i].Subject {
		case "m":
			m = &report.Rows[i]
		case "n":
			n = &report.Rows[i]
		}
	}
	if m == nil || n == nil {
		t.Fatalf("missing subjects: %+v", report.Rows)
	}
	if m.Count != 2 || m.SuccessRate < 0.49 || m.SuccessRate > 0.51 {
		t.Errorf("m: count=%d success_rate=%v", m.Count, m.SuccessRate)
	}
	if m.AvgLatencyMs < 199 || m.AvgLatencyMs > 201 {
		t.Errorf("m avg latency = %v, want ~200ms", m.AvgLatencyMs)
	}
	if n.Count != 1 {
		t.Errorf("n: count=%d, want 1", n.Count)
	}
}

// TestTTP95WeightedBucket covers B-14: when one bucket represents 100× more
// observations than another, the overall P95 must lean toward the heavier
// bucket's distribution. With equal bucket weights the previous reservoir-union
// approach produced a bucket-weighted (not request-weighted) percentile.
func TestTTP95WeightedBucket(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100000, 24*time.Hour)
	heavy := "heavy"
	light := "light"
	for i := 0; i < 10; i++ {
		at := now.Add(-time.Duration(5+i) * time.Minute)
		for j := 0; j < 1000; j++ {
			a.IngestDirect(aggregator.Sample{Provider: "p", Model: heavy, AuthID: "one", RequestedAt: at.Add(time.Duration(j) * time.Microsecond), Latency: time.Second, TTFT: 10 * time.Millisecond})
		}
		for j := 0; j < 10; j++ {
			a.IngestDirect(aggregator.Sample{Provider: "p", Model: light, AuthID: "two", RequestedAt: at.Add(time.Duration(j) * time.Millisecond), Latency: time.Second, TTFT: 500 * time.Millisecond})
		}
	}
	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: a.Timeline(time.Minute),
	}
	report, err := BuildTimeline(a.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{heavy, light}, Range: "1h", Metric: MetricP95TTFT}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	var heavyRow, lightRow *Row
	for i := range report.Rows {
		switch report.Rows[i].Subject {
		case heavy:
			heavyRow = &report.Rows[i]
		case light:
			lightRow = &report.Rows[i]
		}
	}
	if heavyRow == nil || lightRow == nil {
		t.Fatalf("missing subjects: %+v", report.Rows)
	}
	if heavyRow.P95TTFTMs >= 50 {
		t.Errorf("heavy subject P95 = %v, want <50ms (request-weighted)", heavyRow.P95TTFTMs)
	}
	if lightRow.P95TTFTMs < 400 {
		t.Errorf("light subject P95 = %v, want ~500ms", lightRow.P95TTFTMs)
	}
}

// TestTTP95OrderIndependent covers B-14 follow-up: the weighted P95 must not
// depend on the order in which contributing buckets arrive. Reverse the slice
// and confirm the result is statistically identical (allow small jitter from
// random reservoir sampling).
func TestTTP95OrderIndependent(t *testing.T) {
	mkContrib := func(weight uint64, value time.Duration) ttftContribution {
		samples := make([]time.Duration, 64)
		for i := range samples {
			samples[i] = value
		}
		return ttftContribution{samples: samples, weight: weight}
	}
	forward := []ttftContribution{mkContrib(100000, 10*time.Millisecond), mkContrib(100, 500*time.Millisecond)}
	reverse := []ttftContribution{mkContrib(100, 500*time.Millisecond), mkContrib(100000, 10*time.Millisecond)}
	weight := uint64(100100)
	p1 := weightedSample(forward, weight)
	p2 := weightedSample(reverse, weight)
	if len(p1) == 0 || len(p2) == 0 {
		t.Fatal("weightedSample returned empty")
	}
	p95f := percentileAt(p1, 0.95)
	p95r := percentileAt(p2, 0.95)
	if p95f > 20*time.Millisecond {
		t.Errorf("forward p95 = %v, want ~10ms (heavy subject dominates)", p95f)
	}
	if p95r > 20*time.Millisecond {
		t.Errorf("reverse p95 = %v, want ~10ms (heavy subject dominates)", p95r)
	}
}

func percentileAt(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	samples := make([]time.Duration, len(values))
	copy(samples, values)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := int(p * float64(len(samples)-1))
	return samples[idx]
}

// TestTTP95Cap16K covers B-14 follow-up: the merged reservoir must be bounded
// by maxTTP95Samples even when the natural downsample step is 1.
func TestTTP95Cap16K(t *testing.T) {
	const reservoirSize = 1024
	a := accumulator{}
	for i := 0; i < 1000; i++ {
		b := aggregator.NewBucket(time.Minute, time.Unix(int64(i), 0))
		for j := 0; j < reservoirSize; j++ {
			b.TTFTReservoirAddForTest(time.Duration(j+1) * time.Millisecond)
		}
		a.add(b)
	}
	if len(a.ttftContribs) == 0 {
		t.Fatal("expected ttftContribs to be populated")
	}
	merged := weightedSample(a.ttftContribs, a.ttftObserved)
	if len(merged) > maxTTP95Samples {
		t.Errorf("merged = %d, exceeds cap %d", len(merged), maxTTP95Samples)
	}
}

// TestTTP95SameSubjectWeightDiff covers B-14: when a single subject has
// multiple buckets whose observation counts differ by 100×, the merged P95 must
// lean toward the heavier bucket's distribution. This is the regression
// reviewer called out: previous tests only compared two different subjects, so
// they could not detect bucket-weight bias within a single subject.
func TestTTP95SameSubjectWeightDiff(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100000, 24*time.Hour)
	subject := "shared"
	// 10 buckets for the heavy tail: 1000 observations each with TTFT=10ms.
	for i := 0; i < 10; i++ {
		at := now.Add(-time.Duration(5+i) * time.Minute)
		for j := 0; j < 1000; j++ {
			a.IngestDirect(aggregator.Sample{
				Provider: "p", Model: subject, AuthID: "x",
				RequestedAt: at.Add(time.Duration(j) * time.Microsecond),
				Latency:     time.Second, TTFT: 10 * time.Millisecond,
			})
		}
		// Same bucket also contributes a single 500ms observation.
		a.IngestDirect(aggregator.Sample{
			Provider: "p", Model: subject, AuthID: "x",
			RequestedAt: at.Add(500 * time.Millisecond),
			Latency:     time.Second, TTFT: 500 * time.Millisecond,
		})
	}
	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: a.Timeline(time.Minute),
	}
	report, err := BuildTimeline(a.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{subject, "placeholder"}, Range: "1h", Metric: MetricP95TTFT}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(report.Rows))
	}
	var row *Row
	for i := range report.Rows {
		if report.Rows[i].Subject == subject {
			row = &report.Rows[i]
		}
	}
	if row == nil {
		t.Fatal("missing subject row")
	}
	p95 := row.P95TTFTMs
	if p95 >= 100 {
		t.Errorf("P95 = %vms, want ~10ms (heavy 10ms bucket dominates 500ms outliers)", p95)
	}
}

// TestTTP95ReservoirTruncation covers B-14 follow-up: when a bucket's allocated
// share exceeds its reservoir size, sampling with replacement must still
// preserve the bucket's weight in the merged distribution.
func TestTTP95ReservoirTruncation(t *testing.T) {
	mkContrib := func(weight uint64, value time.Duration) ttftContribution {
		return ttftContribution{samples: []time.Duration{value}, weight: weight}
	}
	contribs := []ttftContribution{
		mkContrib(100000, 10*time.Millisecond),
		mkContrib(100, 500*time.Millisecond),
	}
	weight := uint64(100100)
	for i := 0; i < 5; i++ {
		merged := weightedSample(contribs, weight)
		if len(merged) == 0 {
			t.Fatal("weightedSample returned empty")
		}
		if len(merged) > maxTTP95Samples {
			t.Fatalf("merged %d > cap %d", len(merged), maxTTP95Samples)
		}
		p95 := percentileAt(merged, 0.95)
		if p95 > 50*time.Millisecond {
			t.Errorf("P95 = %v, want <=50ms (heavy bucket should dominate)", p95)
		}
	}
}

// TestTTP95StrictCap16KPlusOne covers B-14 strict-cap: with 16K+1 buckets
// contributing a single sample each, the merged sample must be strictly bounded
// by maxTTP95Samples (not 16K+1).
func TestTTP95StrictCap16KPlusOne(t *testing.T) {
	a := accumulator{}
	buckets := maxTTP95Samples + 1
	for i := 0; i < buckets; i++ {
		b := aggregator.NewBucket(time.Minute, time.Unix(int64(i), 0))
		b.TTFTReservoirAddForTest(time.Duration(i+1) * time.Microsecond)
		a.add(b)
	}
	if len(a.ttftContribs) != buckets {
		t.Fatalf("contribs = %d, want %d", len(a.ttftContribs), buckets)
	}
	merged := weightedSample(a.ttftContribs, a.ttftObserved)
	if len(merged) > maxTTP95Samples {
		t.Errorf("merged = %d, exceeds strict cap %d", len(merged), maxTTP95Samples)
	}
	if len(merged) == 0 {
		t.Error("merged is empty")
	}
}

// TestNormalizeConstantSeries covers S-06: when every subject's series has the
// same value at a time point, the normalized chart point must be 0.5 (not NaN
// or 0 due to division-by-zero in min/max normalization).
func TestNormalizeConstantSeries(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(100, 24*time.Hour)
	for i := 0; i < 5; i++ {
		at := now.Add(-time.Duration(5+i) * time.Minute)
		for _, m := range []string{"a", "b"} {
			a.IngestDirect(aggregator.Sample{Provider: "p", Model: m, AuthID: "x", RequestedAt: at, Latency: time.Second, TTFT: 50 * time.Millisecond})
		}
	}
	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: a.Timeline(time.Minute),
	}
	report, err := BuildTimeline(a.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{"a", "b"}, Range: "1h", Metric: MetricP95TTFT}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d", len(report.Rows))
	}
	// Both subjects have identical TTFT=50ms at each time point. Series.Raw
	// (raw TTFT) is the raw value emitted per row. The normalize step (max==min
	// branch in app.js) maps this to 0.5 in the chart. Here we verify the
	// upstream contract: raw values match, so chart code can detect constant
	// series and render 0.5.
	for i := 0; i < len(report.Rows[0].Series); i++ {
		a0 := report.Rows[0].Series[i]
		b0 := report.Rows[1].Series[i]
		if a0.Value != b0.Value {
			t.Errorf("subjects differ at %v: %v vs %v", a0.At, a0.Value, b0.Value)
		}
	}
}

// TestSaveRestoreTTFTP95 covers B-15: the TTFT reservoir must survive a
// save → restore → BuildTimeline cycle so that Compare reports non-zero P95
// after a process restart. Before B-15, the persisted schema omitted the
// reservoir and restored buckets reported TTFT P95 = 0.
func TestSaveRestoreTTFTP95(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "stats.json")
	store := persist.NewStore(path)

	src := aggregator.New(1000, 24*time.Hour)
	for i := 0; i < 200; i++ {
		src.IngestDirect(aggregator.Sample{
			Provider:    "p",
			Model:       "m",
			AuthID:      "x",
			RequestedAt: now.Add(-time.Duration(i+1) * time.Second),
			Latency:     time.Second,
			TTFT:        50 * time.Millisecond,
		})
	}
	if err := store.Save(src); err != nil {
		t.Fatal(err)
	}

	restored := aggregator.New(1000, 24*time.Hour)
	if err := store.Load(restored, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawBytes), "ttft_reservoir_ns") {
		t.Error("persisted snapshot must include ttft_reservoir_ns field")
	}

	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute: restored.Timeline(time.Minute),
	}
	report, err := BuildTimeline(restored.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{"m", "n"}, Range: "1h", Metric: MetricP95TTFT}, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	var m *Row
	for i := range report.Rows {
		if report.Rows[i].Subject == "m" {
			m = &report.Rows[i]
		}
	}
	if m == nil {
		t.Fatal("missing m row")
	}
	if m.P95TTFTMs < 49 || m.P95TTFTMs > 51 {
		t.Errorf("restored P95 = %v, want ~50ms (B-15 regression check)", m.P95TTFTMs)
	}
}

// TestTrend24hOver24h covers S-09 option A: trend is computed against the most
// recent 24h and the 24h before that, regardless of the report range. With
// only data in the most recent 24h, the trend should be reported as +1.0
// (current=metric, previous=0) but our contract zeroes the field when the
// previous denominator is zero, to avoid fabricating a 100% jump signal.
func TestTrend24hOver24h(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	a := aggregator.New(1000, 48*time.Hour)
	// 30 hours ago: latency=200ms (lives in "previous 24h" only)
	for i := 0; i < 10; i++ {
		a.IngestDirect(aggregator.Sample{Provider: "p", Model: "m", AuthID: "x", RequestedAt: now.Add(-30 * time.Hour).Add(time.Duration(i) * time.Second), Latency: 200 * time.Millisecond})
	}
	// 2 hours ago: latency=100ms (lives in "current 24h" only)
	for i := 0; i < 10; i++ {
		a.IngestDirect(aggregator.Sample{Provider: "p", Model: "m", AuthID: "x", RequestedAt: now.Add(-2 * time.Hour).Add(time.Duration(i) * time.Second), Latency: 100 * time.Millisecond})
	}
	timeline := map[time.Duration]map[time.Time]map[string]*aggregator.Bucket{
		time.Minute:   a.Timeline(time.Minute),
		15 * time.Minute: a.Timeline(15 * time.Minute),
	}
	report, err := BuildTimeline(a.Snapshot(), timeline, Request{Kind: KindModel, IDs: []string{"m", "n"}, Range: "1h", Metric: MetricAvgLatency}, 48*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	var m *Row
	for i := range report.Rows {
		if report.Rows[i].Subject == "m" {
			m = &report.Rows[i]
		}
	}
	if m == nil {
		t.Fatal("missing m")
	}
	// current=100, previous=200 -> trend = (100-200)/200 = -0.5
	if m.Trend24hOver24h > -0.49 || m.Trend24hOver24h < -0.51 {
		t.Errorf("trend = %v, want -0.5", m.Trend24hOver24h)
	}
}
