package aggregator

import (
	"sync"
	"testing"
	"time"
)

func TestIngestAccumulates(t *testing.T) {
	a := New(1000, time.Hour)
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		a.ingest(Sample{
			Provider:    "openai",
			Model:       "gpt-5.5",
			Alias:       "gpt-5.5",
			AuthID:      "auth-1",
			RequestedAt: base.Add(time.Duration(i) * time.Second),
			Latency:     time.Duration(100+i*10) * time.Millisecond,
			TTFT:        time.Duration(20+i) * time.Millisecond,
			Failed:      i == 9,
			InputTokens: 100,
			OutputTokens: 50,
		})
	}

	snap := a.Snapshot()
	m := snap[time.Minute]
	if len(m) != 1 {
		t.Fatalf("expected 1 series, got %d", len(m))
	}
	for _, b := range m {
		if b.Count != 10 {
			t.Errorf("count = %d, want 10", b.Count)
		}
		if b.Failed != 1 {
			t.Errorf("failed = %d, want 1", b.Failed)
		}
		if b.SumInput != 1000 {
			t.Errorf("sum_input = %d, want 1000", b.SumInput)
		}
		if b.SumOutput != 500 {
			t.Errorf("sum_output = %d, want 500", b.SumOutput)
		}
		expectedLatency := time.Duration(0)
		for i := 0; i < 10; i++ {
			expectedLatency += time.Duration(100+i*10) * time.Millisecond
		}
		if b.SumLatency != expectedLatency {
			t.Errorf("sum_latency = %v, want %v", b.SumLatency, expectedLatency)
		}
	}
}

func TestWindowTruncation(t *testing.T) {
	a := New(1000, time.Hour)
	base := time.Date(2026, 7, 22, 10, 3, 45, 0, time.UTC)

	a.ingest(Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-1",
		RequestedAt: base,
		Latency:     100 * time.Millisecond,
	})

	snap := a.Snapshot()

	b1m := snap[time.Minute]
	for _, b := range b1m {
		expected := base.Truncate(time.Minute)
		if !b.Start.Equal(expected) {
			t.Errorf("1m bucket start = %v, want %v", b.Start, expected)
		}
	}

	b5m := snap[5*time.Minute]
	for _, b := range b5m {
		expected := base.Truncate(5 * time.Minute)
		if !b.Start.Equal(expected) {
			t.Errorf("5m bucket start = %v, want %v", b.Start, expected)
		}
	}
}

func TestPercentile(t *testing.T) {
	b := NewBucket(time.Minute, time.Now())
	for i := 1; i <= 100; i++ {
		b.Accumulate(Sample{
			Latency:     time.Duration(i) * time.Millisecond,
			RequestedAt: time.Now(),
		})
	}

	p50 := b.Percentile(0.50)
	if p50 < 49*time.Millisecond || p50 > 52*time.Millisecond {
		t.Errorf("p50 = %v, want ~50ms", p50)
	}

	p95 := b.Percentile(0.95)
	if p95 < 94*time.Millisecond || p95 > 97*time.Millisecond {
		t.Errorf("p95 = %v, want ~95ms", p95)
	}
}

func TestCardinalityLimit(t *testing.T) {
	a := New(3, time.Hour)
	base := time.Now()

	for i := 0; i < 5; i++ {
		a.ingest(Sample{
			Provider:    "openai",
			Model:       "gpt-5.5",
			AuthID:      "auth-" + string(rune('a'+i)),
			RequestedAt: base.Add(time.Duration(i) * time.Second),
			Latency:     time.Millisecond,
		})
	}

	snap := a.Snapshot()
	m := snap[time.Minute]
	if len(m) > 3 {
		t.Errorf("series count = %d, want <= 3", len(m))
	}
}

func TestRetentionEviction(t *testing.T) {
	a := New(1000, 5*time.Minute)
	old := time.Now().Add(-10 * time.Minute)

	a.ingest(Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-1",
		RequestedAt: old,
		Latency:     time.Millisecond,
	})

	a.ingest(Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-2",
		RequestedAt: time.Now(),
		Latency:     time.Millisecond,
	})

	a.evictExpired()

	snap := a.Snapshot()
	m := snap[time.Minute]
	if len(m) != 1 {
		t.Errorf("after eviction, series count = %d, want 1", len(m))
	}
}

func TestStreamRate(t *testing.T) {
	s := Sample{
		Latency:      2 * time.Second,
		TTFT:         500 * time.Millisecond,
		OutputTokens: 300,
	}
	rate, ok := s.StreamRate()
	if !ok {
		t.Fatal("expected valid stream rate")
	}
	expected := 300.0 / 1.5
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("stream rate = %f, want %f", rate, expected)
	}
}

func TestStreamRateNonStreaming(t *testing.T) {
	s := Sample{
		Latency:      2 * time.Second,
		TTFT:         0,
		OutputTokens: 200,
	}
	rate, ok := s.StreamRate()
	if !ok {
		t.Fatal("expected valid stream rate for non-streaming")
	}
	if rate < 99.9 || rate > 100.1 {
		t.Errorf("stream rate = %f, want 100", rate)
	}
}

func TestConcurrentIngest(t *testing.T) {
	a := New(50000, time.Hour)
	a.Start(t.Context())
	defer a.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				a.Ingest(Sample{
					Provider:    "openai",
					Model:       "gpt-5.5",
					AuthID:      "auth-1",
					RequestedAt: time.Now(),
					Latency:     time.Millisecond,
				})
			}
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	snap := a.Snapshot()
	m := snap[time.Minute]
	var total uint64
	for _, b := range m {
		total += b.Count
	}
	if total != 1000 {
		t.Errorf("total count = %d, want 1000", total)
	}
}

func TestSeriesKeyEscaping(t *testing.T) {
	s := Sample{
		Provider: "open|ai",
		Model:    "gpt\\5.5",
		Alias:    "alias",
		AuthID:   "auth-1",
	}
	key := s.SeriesKey()
	parts := SplitSeriesKey(key)
	if parts[0] != "open|ai" {
		t.Errorf("provider = %q, want %q", parts[0], "open|ai")
	}
	if parts[1] != "gpt\\5.5" {
		t.Errorf("model = %q, want %q", parts[1], "gpt\\5.5")
	}
}

func TestReset(t *testing.T) {
	a := New(1000, time.Hour)
	a.ingest(Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-1",
		RequestedAt: time.Now(),
		Latency:     time.Millisecond,
	})
	a.Reset()
	snap := a.Snapshot()
	for _, m := range snap {
		if len(m) != 0 {
			t.Errorf("after reset, window has %d series", len(m))
		}
	}
}
