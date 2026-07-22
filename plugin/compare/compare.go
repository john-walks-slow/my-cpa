package compare

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
)

const maxTTP95Samples = 16 * 1024

const (
	KindModel         = "model"
	KindAuth          = "auth"
	MetricP95TTFT     = "p95_ttft_ms"
	MetricTPS         = "avg_stream_rate_tps"
	MetricSuccessRate = "success_rate"
	MetricAvgLatency  = "avg_latency_ms"
)

type Request struct {
	Kind, Range, From, To, Metric string
	IDs                           []string
}
type Subject struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}
type Range struct {
	Preset string `json:"preset"`
	From   string `json:"from"`
	To     string `json:"to"`
}
type Point struct {
	At    string  `json:"at"`
	Value float64 `json:"value"`
	Raw   float64 `json:"raw_ttft_ms,omitempty"`
}
type Row struct {
	Subject         string  `json:"subject"`
	Label           string  `json:"label"`
	Count           uint64  `json:"count"`
	SuccessRate     float64 `json:"success_rate"`
	P95TTFTMs       float64 `json:"p95_ttft_ms"`
	TTFTObserved    uint64  `json:"ttft_observed"`
	AvgStreamRate   float64 `json:"avg_stream_rate_tps"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	Trend24hOver24h float64 `json:"trend_24h_over_24h"`
	Series          []Point `json:"series"`
}
type Report struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   string    `json:"generated_at"`
	Title         string    `json:"title"`
	Range         Range     `json:"range"`
	Metric        string    `json:"metric"`
	Subjects      []Subject `json:"subjects"`
	Rows          []Row     `json:"rows"`
}

func ValidateRequest(req Request, retention time.Duration, now time.Time) (Range, error) {
	if req.Kind != KindModel && req.Kind != KindAuth {
		return Range{}, fmt.Errorf("kind must be model or auth")
	}
	if len(req.IDs) < 2 || len(req.IDs) > 6 {
		return Range{}, fmt.Errorf("select 2 to 6 subjects")
	}
	seen := map[string]struct{}{}
	for i := range req.IDs {
		req.IDs[i] = strings.TrimSpace(req.IDs[i])
		if req.IDs[i] == "" {
			return Range{}, fmt.Errorf("subject id cannot be empty")
		}
		if _, ok := seen[req.IDs[i]]; ok {
			return Range{}, fmt.Errorf("duplicate subject id")
		}
		seen[req.IDs[i]] = struct{}{}
	}
	if req.Metric == "" {
		req.Metric = MetricP95TTFT
	}
	if !validMetric(req.Metric) {
		return Range{}, fmt.Errorf("invalid metric")
	}
	return ParseRange(req.Range, req.From, req.To, retention, now)
}
func Build(snapshot map[time.Duration]map[string]*aggregator.Bucket, req Request, retention time.Duration, now time.Time) (Report, error) {
	return buildReport(snapshot, nil, req, retention, now)
}
func BuildTimeline(snapshot map[time.Duration]map[string]*aggregator.Bucket, timeline map[time.Duration]map[time.Time]map[string]*aggregator.Bucket, req Request, retention time.Duration, now time.Time) (Report, error) {
	return buildReport(snapshot, timeline, req, retention, now)
}
func buildReport(snapshot map[time.Duration]map[string]*aggregator.Bucket, timeline map[time.Duration]map[time.Time]map[string]*aggregator.Bucket, req Request, retention time.Duration, now time.Time) (Report, error) {
	rng, err := ValidateRequest(req, retention, now)
	if err != nil {
		return Report{}, err
	}
	if req.Metric == "" {
		req.Metric = MetricP95TTFT
	}
	if req.Range == "" {
		req.Range = "24h"
	}
	result := Report{SchemaVersion: 1, GeneratedAt: now.UTC().Format(time.RFC3339), Title: "Model comparison", Range: rng, Metric: req.Metric, Subjects: make([]Subject, 0, len(req.IDs)), Rows: make([]Row, 0, len(req.IDs))}
	for _, id := range req.IDs {
		id = strings.TrimSpace(id)
		result.Subjects = append(result.Subjects, Subject{Kind: req.Kind, ID: id, Label: id})
		result.Rows = append(result.Rows, buildRow(snapshot, timeline, req.Kind, id, rng, req.Metric, now))
	}
	return result, nil
}
func ParseRange(preset, fromText, toText string, retention time.Duration, now time.Time) (Range, error) {
	if preset == "" {
		preset = "24h"
	}
	to := now.UTC()
	var from time.Time
	switch preset {
	case "1h", "6h", "24h":
		d, _ := time.ParseDuration(preset)
		from = to.Add(-d)
	case "7d":
		from = to.Add(-7 * 24 * time.Hour)
	case "custom":
		var err error
		from, err = time.Parse(time.RFC3339, fromText)
		if err != nil {
			return Range{}, fmt.Errorf("invalid from")
		}
		to, err = time.Parse(time.RFC3339, toText)
		if err != nil {
			return Range{}, fmt.Errorf("invalid to")
		}
		from, to = from.UTC(), to.UTC()
	default:
		return Range{}, fmt.Errorf("invalid range")
	}
	if !from.Before(to) {
		return Range{}, fmt.Errorf("range must have from before to")
	}
	if to.Sub(from) > retention {
		return Range{}, fmt.Errorf("range exceeds retention")
	}
	return Range{Preset: preset, From: from.Format(time.RFC3339), To: to.Format(time.RFC3339)}, nil
}
func validMetric(metric string) bool {
	return metric == MetricP95TTFT || metric == MetricTPS || metric == MetricSuccessRate || metric == MetricAvgLatency
}
func rangeWindow(preset string) time.Duration {
	switch preset {
	case "1h", "custom":
		return time.Minute
	case "6h":
		return 5 * time.Minute
	case "24h":
		return 15 * time.Minute
	case "7d":
		return time.Hour
	default:
		return time.Minute
	}
}

// trendWindow returns the bucket size used for the 24h-over-24h trend. The trend
// is always the most recent 24h vs the 24h before that, regardless of the
// report's primary range. We pick a window that resolves the 24h span into
// enough buckets to be statistically meaningful without exploding memory.
func trendWindow() time.Duration { return 15 * time.Minute }

// trendBoundaries returns the [currentFrom, currentTo, previousFrom, previousTo)
// windows in absolute time for trend computation. Both spans are 24h long and
// together cover the most recent 48h.
func trendBoundaries(now time.Time) (time.Time, time.Time, time.Time, time.Time) {
	currentTo := now.UTC()
	currentFrom := currentTo.Add(-24 * time.Hour)
	previousFrom := currentFrom.Add(-24 * time.Hour)
	return currentFrom, currentTo, previousFrom, currentFrom
}

func buildRow(snapshot map[time.Duration]map[string]*aggregator.Bucket, timeline map[time.Duration]map[time.Time]map[string]*aggregator.Bucket, kind, id string, rng Range, metric string, now time.Time) Row {
	from, _ := time.Parse(time.RFC3339, rng.From)
	to, _ := time.Parse(time.RFC3339, rng.To)
	row := Row{Subject: id, Label: id, Series: []Point{}}
	window := rangeWindow(rng.Preset)
	var current, previous accumulator
	points := map[time.Time]*accumulator{}
	if timeline != nil {
		for at, buckets := range timeline[window] {
			if at.Before(from.Add(-24*time.Hour)) || !at.Before(to) {
				continue
			}
			for key, b := range buckets {
				if !matches(key, kind, id) {
					continue
				}
				if at.Before(from) {
					previous.add(b)
				} else {
					if points[at] == nil {
						points[at] = &accumulator{}
					}
					points[at].add(b)
					current.add(b)
				}
			}
		}
	} else {
		for key, b := range snapshot[window] {
			if !matches(key, kind, id) || b.Start.Before(from) || !b.Start.Before(to) {
				continue
			}
			current.add(b)
			points[b.Start] = &accumulator{}
			points[b.Start].add(b)
		}
	}
	row.Count = current.count
	row.SuccessRate = current.successRate()
	row.P95TTFTMs = current.p95TTFT()
	row.TTFTObserved = current.ttftObserved
	row.AvgStreamRate = current.avgTPS()
	row.AvgLatencyMs = current.avgLatency()
	row.Trend24hOver24h = computeTrend24hOver24h(timeline, window, kind, id, metric, now)
	starts := make([]time.Time, 0, len(points))
	for at := range points {
		starts = append(starts, at)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	for _, at := range starts {
		row.Series = append(row.Series, Point{At: at.UTC().Format(time.RFC3339), Value: points[at].metric(metric), Raw: points[at].rawTTFTMs()})
	}
	return row
}

// computeTrend24hOver24h aggregates the most recent 24h vs the previous 24h
// using the same bucket window as the report's primary range, then returns the
// relative change for the requested metric. This is intentionally decoupled
// from the report range: the field name is trend_24h_over_24h, so the windows
// must be fixed at 24h regardless of whether the user picked 1h or 7d.
func computeTrend24hOver24h(timeline map[time.Duration]map[time.Time]map[string]*aggregator.Bucket, window time.Duration, kind, id, metric string, now time.Time) float64 {
	currentFrom, currentTo, previousFrom, _ := trendBoundaries(now)
	var current, previous accumulator
	if timeline != nil {
		for at, buckets := range timeline[window] {
			if at.Before(previousFrom) || !at.Before(currentTo) {
				continue
			}
			for key, b := range buckets {
				if !matches(key, kind, id) {
					continue
				}
				if at.Before(currentFrom) {
					previous.add(b)
				} else {
					current.add(b)
				}
			}
		}
	}
	old := previous.metric(metric)
	newVal := current.metric(metric)
	if old == 0 {
		return 0
	}
	return (newVal - old) / old
}

type ttftContribution struct {
	samples []time.Duration
	weight  uint64
}

// accumulator aggregates raw counts and a list of TTFT reservoir contributions.
// TTFT P95 sampling is deferred until p95TTFT() so the final reservoir is
// weighted by the global observation total rather than incrementally as each
// bucket arrives; this makes the resulting percentile order-independent.
type accumulator struct {
	count, failed, streamCount     uint64
	sumLatency, sumTTFT, streamSum float64
	ttftObserved                   uint64
	ttftContribs                   []ttftContribution
}

func (a *accumulator) add(b *aggregator.Bucket) {
	a.count += b.Count
	a.failed += b.Failed
	a.sumLatency += float64(b.SumLatency.Microseconds()) / 1000
	a.sumTTFT += float64(b.SumTTFT.Microseconds()) / 1000
	a.streamCount += b.StreamRateCount
	a.streamSum += b.StreamRateSum
	weight := b.TTFTReservoirCount()
	if weight == 0 {
		return
	}
	samples := b.TTFTReservoir()
	if len(samples) == 0 {
		return
	}
	a.ttftObserved += weight
	a.ttftContribs = append(a.ttftContribs, ttftContribution{samples: samples, weight: weight})
}
func (a accumulator) successRate() float64 {
	if a.count == 0 {
		return 0
	}
	return float64(a.count-a.failed) / float64(a.count)
}
func (a accumulator) p95TTFT() float64 {
	if a.ttftObserved == 0 || len(a.ttftContribs) == 0 {
		return 0
	}
	merged := weightedSample(a.ttftContribs, a.ttftObserved)
	if len(merged) > maxTTP95Samples {
		merged = merged[:maxTTP95Samples]
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
	idx := int(0.95 * float64(len(merged)-1))
	return float64(merged[idx].Microseconds()) / 1000
}
func (a accumulator) hasTTFTSamples() bool { return a.ttftObserved > 0 }
func (a accumulator) rawTTFTMs() float64 {
	return a.p95TTFT()
}
func (a accumulator) avgTPS() float64 {
	if a.streamCount == 0 {
		return 0
	}
	return a.streamSum / float64(a.streamCount)
}
func (a accumulator) avgLatency() float64 {
	if a.count == 0 {
		return 0
	}
	return a.sumLatency / float64(a.count)
}
func (a accumulator) metric(metric string) float64 {
	switch metric {
	case MetricTPS:
		return a.avgTPS()
	case MetricSuccessRate:
		return a.successRate()
	case MetricAvgLatency:
		return a.avgLatency()
	default:
		return a.p95TTFT()
	}
}
func matches(key, kind, id string) bool {
	p := aggregator.SplitSeriesKey(key)
	if kind == KindAuth {
		return p[3] == id
	}
	return p[1] == id || p[2] == id || p[0]+"|"+p[1]+"|"+p[2] == id
}

// weightedSample produces a request-weighted sample of TTFT observations from
// every contributing bucket. Each bucket's reservoir is sub-sampled in
// proportion to its weight relative to the global observed total, so the merged
// distribution reflects how often each observation actually occurred rather
// than how many buckets contributed. The output is bounded by maxTTP95Samples.
//
// When a bucket's allocated share exceeds the size of its reservoir, the
// reservoir is sampled with replacement: each draw is independent, so the
// bucket's effective vote count in the merged sample reflects its observation
// weight rather than its reservoir cardinality. Without this, a bucket with
// 1000× more observations but the same reservoir size would lose its weight.
//
// The algorithm is intentionally order-independent: shares are computed from
// the global weight sum and applied uniformly, so inserting buckets in any
// order yields the same expected sample.
func weightedSample(contribs []ttftContribution, totalWeight uint64) []time.Duration {
	if len(contribs) == 0 || totalWeight == 0 {
		return nil
	}
	shares := make([]int, len(contribs))
	allocated := 0
	for i, c := range contribs {
		s := int(uint64(maxTTP95Samples) * c.weight / totalWeight)
		if s < 1 {
			s = 1
		}
		shares[i] = s
		allocated += s
	}
	if allocated > maxTTP95Samples {
		for i := range shares {
			scaled := shares[i] * maxTTP95Samples / allocated
			if scaled < 1 && contribs[i].weight > 0 {
				scaled = 1
			}
			shares[i] = scaled
		}
		allocated = 0
		for _, s := range shares {
			allocated += s
		}
	}
	merged := make([]time.Duration, 0, allocated)
	for i, c := range contribs {
		n := shares[i]
		if n <= len(c.samples) {
			perm := rand.Perm(len(c.samples))
			for j := 0; j < n; j++ {
				merged = append(merged, c.samples[perm[j]])
			}
			continue
		}
		// Sample with replacement when the allocated share exceeds the reservoir
		// size; this preserves the bucket's vote count in the merged distribution.
		for j := 0; j < n; j++ {
			merged = append(merged, c.samples[rand.IntN(len(c.samples))])
		}
	}
	if len(merged) > maxTTP95Samples {
		merged = merged[:maxTTP95Samples]
	}
	return merged
}