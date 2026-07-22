package persist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
)

// schemaVersion is the current on-disk schema. v2 adds the per-bucket TTFT
// reservoir so P95 can survive a process restart; v1 files still load but
// without the reservoir (TTFT P95 reports zero until new observations arrive).
const schemaVersion = 2

type snapshot struct {
	SchemaVersion int                          `json:"schema_version"`
	SavedAt       time.Time                    `json:"saved_at"`
	Windows       map[string]map[string]bucketJSON `json:"windows"`
}

type bucketJSON struct {
	Start           time.Time `json:"start"`
	Count           uint64    `json:"count"`
	Failed          uint64    `json:"failed"`
	SumLatencyMs    int64     `json:"sum_latency_ms"`
	SumTTFTMs       int64     `json:"sum_ttft_ms"`
	SumOutput       int64     `json:"sum_output"`
	SumInput        int64     `json:"sum_input"`
	SumReasoning    int64     `json:"sum_reasoning"`
	SumCached       int64     `json:"sum_cached"`
	StreamRateSum   float64   `json:"stream_rate_sum"`
	StreamRateCount uint64    `json:"stream_rate_count"`
	LastSampleAt    time.Time `json:"last_sample_at"`

	// v2: TTFT reservoir persistence. Stored as nanoseconds for lossless
	// round-tripping; missing/empty fields are tolerated by old builds.
	TTFTReservoirNs    []int64 `json:"ttft_reservoir_ns,omitempty"`
	TTFTReservoirCount uint64  `json:"ttft_reservoir_count,omitempty"`
}

var windowNames = map[time.Duration]string{
	time.Minute:        "1m",
	5 * time.Minute:    "5m",
	15 * time.Minute:   "15m",
	time.Hour:          "1h",
	24 * time.Hour:     "24h",
}

var windowByName = func() map[string]time.Duration {
	m := make(map[string]time.Duration, len(windowNames))
	for d, n := range windowNames {
		m[n] = d
	}
	return m
}()

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Save(agg *aggregator.Aggregator) error {
	snap := snapshot{
		SchemaVersion: schemaVersion,
		SavedAt:       time.Now(),
		Windows:       make(map[string]map[string]bucketJSON),
	}
	data := agg.Snapshot()
	for w, m := range data {
		name := windowNames[w]
		wm := make(map[string]bucketJSON, len(m))
		for k, b := range m {
			ttft := b.TTFTReservoir()
			ttftNs := make([]int64, 0, len(ttft))
			for _, v := range ttft {
				ttftNs = append(ttftNs, int64(v))
			}
			wm[k] = bucketJSON{
				Start:             b.Start,
				Count:             b.Count,
				Failed:            b.Failed,
				SumLatencyMs:      b.SumLatency.Milliseconds(),
				SumTTFTMs:         b.SumTTFT.Milliseconds(),
				SumOutput:         b.SumOutput,
				SumInput:          b.SumInput,
				SumReasoning:      b.SumReasoning,
				SumCached:         b.SumCached,
				StreamRateSum:     b.StreamRateSum,
				StreamRateCount:   b.StreamRateCount,
				LastSampleAt:      b.LastSampleAt,
				TTFTReservoirNs:   ttftNs,
				TTFTReservoirCount: b.TTFTReservoirCount(),
			}
		}
		snap.Windows[name] = wm
	}

	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("persist marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("persist mkdir: %w", err)
	}
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("persist write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("persist rename: %w", err)
	}
	return nil
}

func (s *Store) Load(agg *aggregator.Aggregator, retention time.Duration) error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("persist read: %w", err)
	}

	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return fmt.Errorf("persist unmarshal: %w", err)
	}
	if snap.SchemaVersion != 1 && snap.SchemaVersion != schemaVersion {
		return fmt.Errorf("persist: unsupported schema version %d", snap.SchemaVersion)
	}
	if time.Since(snap.SavedAt) > retention {
		return nil
	}

	now := time.Now()
	restored := make(map[time.Duration]map[string]*aggregator.Bucket)
	for name, wm := range snap.Windows {
		w, ok := windowByName[name]
		if !ok {
			continue
		}
		m := make(map[string]*aggregator.Bucket, len(wm))
		for k, bj := range wm {
			if now.Sub(bj.Start) > retention+w {
				continue
			}
			b := aggregator.NewBucket(w, bj.Start)
			b.Count = bj.Count
			b.Failed = bj.Failed
			b.SumLatency = time.Duration(bj.SumLatencyMs) * time.Millisecond
			b.SumTTFT = time.Duration(bj.SumTTFTMs) * time.Millisecond
			b.SumOutput = bj.SumOutput
			b.SumInput = bj.SumInput
			b.SumReasoning = bj.SumReasoning
			b.SumCached = bj.SumCached
			b.StreamRateSum = bj.StreamRateSum
			b.StreamRateCount = bj.StreamRateCount
			b.LastSampleAt = bj.LastSampleAt
			if snap.SchemaVersion >= 2 && len(bj.TTFTReservoirNs) > 0 {
				for _, ns := range bj.TTFTReservoirNs {
					b.TTFTReservoirAddForTest(time.Duration(ns))
				}
			}
			m[k] = b
		}
		if len(m) > 0 {
			restored[w] = m
		}
	}
	agg.Restore(restored)
	return nil
}