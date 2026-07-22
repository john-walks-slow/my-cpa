package aggregator

import (
	"math/rand/v2"
	"slices"
	"time"
)

var AllWindows = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	24 * time.Hour,
}

const reservoirSize = 1024

type Bucket struct {
	Window time.Duration
	Start  time.Time

	Count        uint64
	Failed       uint64
	SumLatency   time.Duration
	SumTTFT      time.Duration
	SumOutput    int64
	SumInput     int64
	SumReasoning int64
	SumCached    int64

	StreamRateSum   float64
	StreamRateCount uint64

	LastSampleAt time.Time

	ttftReservoir []time.Duration
	ttftResCount  uint64
	reservoir     []time.Duration
	resCount      uint64
}

func NewBucket(window time.Duration, start time.Time) *Bucket {
	return &Bucket{
		Window:    window,
		Start:     start,
		reservoir: make([]time.Duration, 0, reservoirSize),
	}
}

func (b *Bucket) Accumulate(s Sample) {
	b.Count++
	if s.Failed {
		b.Failed++
	}
	b.SumLatency += s.Latency
	b.SumTTFT += s.TTFT
	b.SumOutput += s.OutputTokens
	b.SumInput += s.InputTokens
	b.SumReasoning += s.ReasoningTokens
	b.SumCached += s.CachedTokens

	if rate, ok := s.StreamRate(); ok {
		b.StreamRateSum += rate
		b.StreamRateCount++
	}

	if s.RequestedAt.After(b.LastSampleAt) {
		b.LastSampleAt = s.RequestedAt
	}

	b.reservoirAdd(s.Latency)
	b.ttftReservoirAdd(s.TTFT)
}

func (b *Bucket) ttftReservoirAdd(v time.Duration) {
	b.ttftResCount++
	if len(b.ttftReservoir) < reservoirSize {
		b.ttftReservoir = append(b.ttftReservoir, v)
		return
	}
	if j := rand.Uint64N(b.ttftResCount); j < reservoirSize {
		b.ttftReservoir[j] = v
	}
}

func (b *Bucket) TTFTP95() time.Duration {
	return percentile(b.ttftReservoir, .95)
}

// TTFTReservoir returns a copy of the per-bucket TTFT reservoir samples.
func (b *Bucket) TTFTReservoir() []time.Duration {
	if len(b.ttftReservoir) == 0 {
		return nil
	}
	out := make([]time.Duration, len(b.ttftReservoir))
	copy(out, b.ttftReservoir)
	return out
}

// TTFTReservoirCount returns the total number of observations represented by
// the TTFT reservoir, including ones that were not retained as samples. This
// is the weight consumers should use when merging multiple bucket reservoirs
// to compute a request-weighted overall percentile.
func (b *Bucket) TTFTReservoirCount() uint64 { return b.ttftResCount }

// TTFTReservoirAddForTest exposes reservoir insertion for white-box tests; it
// is not used in production code paths.
func (b *Bucket) TTFTReservoirAddForTest(v time.Duration) { b.ttftReservoirAdd(v) }

func (b *Bucket) reservoirAdd(v time.Duration) {
	b.resCount++
	if len(b.reservoir) < reservoirSize {
		b.reservoir = append(b.reservoir, v)
		return
	}
	if j := rand.Uint64N(b.resCount); j < reservoirSize {
		b.reservoir[j] = v
	}
}

func (b *Bucket) Percentile(p float64) time.Duration {
	return percentile(b.reservoir, p)
}

func percentile(values []time.Duration, p float64) time.Duration {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := make([]time.Duration, n)
	copy(sorted, values)
	slices.Sort(sorted)
	idx := int(p * float64(n-1))
	return sorted[idx]
}

func (b *Bucket) AvgLatency() time.Duration {
	if b.Count == 0 {
		return 0
	}
	return b.SumLatency / time.Duration(b.Count)
}

func (b *Bucket) AvgTTFT() time.Duration {
	if b.Count == 0 {
		return 0
	}
	return b.SumTTFT / time.Duration(b.Count)
}

func (b *Bucket) AvgStreamRate() float64 {
	if b.StreamRateCount == 0 {
		return 0
	}
	return b.StreamRateSum / float64(b.StreamRateCount)
}

func (b *Bucket) SuccessRate() float64 {
	if b.Count == 0 {
		return 0
	}
	return float64(b.Count-b.Failed) / float64(b.Count)
}

// Clone returns a deep copy of the bucket, safe for concurrent reads.
func (b *Bucket) Clone() *Bucket {
	cp := *b
	cp.reservoir = make([]time.Duration, len(b.reservoir))
	copy(cp.reservoir, b.reservoir)
	cp.ttftReservoir = make([]time.Duration, len(b.ttftReservoir))
	copy(cp.ttftReservoir, b.ttftReservoir)
	return &cp
}
