package dashboard

import (
	"testing"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
)

func testSample(model, auth string, at time.Time, lat time.Duration, failed bool) aggregator.Sample {
	return aggregator.Sample{
		Provider:     "test",
		Model:        model,
		AuthID:       auth,
		RequestedAt:  at,
		Latency:      lat,
		TTFT:         lat / 2,
		Failed:       failed,
		OutputTokens: 100,
	}
}

func TestComputeInsights(t *testing.T) {
	agg := aggregator.New(1000, time.Hour)
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

	// alpha: fast, uniform latency, no failures
	// beta: slow, uniform latency, 50% failures
	for i := 0; i < 10; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		agg.IngestDirect(testSample("alpha", "auth-a", at, 100*time.Millisecond, false))
		agg.IngestDirect(testSample("beta", "auth-b", at, 1000*time.Millisecond, i < 5))
	}

	ins := ComputeInsights(agg.Snapshot(), base.Add(time.Minute))

	if ins.FastestModel.Model != "alpha" {
		t.Errorf("fastest = %q, want alpha", ins.FastestModel.Model)
	}
	if ins.FastestModel.Unit != "ms" {
		t.Errorf("fastest unit = %q, want ms", ins.FastestModel.Unit)
	}
	if ins.SlowestModel.Model != "beta" {
		t.Errorf("slowest = %q, want beta", ins.SlowestModel.Model)
	}
	if ins.MostStableModel.Model != "alpha" {
		t.Errorf("most stable = %q, want alpha", ins.MostStableModel.Model)
	}
	if ins.LastAnomaly.Model != "beta" {
		t.Errorf("anomaly = %q, want beta", ins.LastAnomaly.Model)
	}
	if ins.LastAnomaly.Value != 50 {
		t.Errorf("anomaly value = %v, want 50", ins.LastAnomaly.Value)
	}
	if ins.LastAnomaly.Unit != "%" {
		t.Errorf("anomaly unit = %q, want %%", ins.LastAnomaly.Unit)
	}
	if ins.GeneratedAt == "" {
		t.Error("expected generated_at to be set")
	}
}

func TestComputeInsightsEmpty(t *testing.T) {
	snap := map[time.Duration]map[string]*aggregator.Bucket{}
	ins := ComputeInsights(snap, time.Now())
	if ins.FastestModel.Model != "" || ins.SlowestModel.Model != "" {
		t.Error("expected empty insights for empty snapshot")
	}
	if ins.GeneratedAt == "" {
		t.Error("expected generated_at to be set even when empty")
	}
}

func TestComputeInsightsNoAnomalyWhenAllSucceed(t *testing.T) {
	agg := aggregator.New(1000, time.Hour)
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		agg.IngestDirect(testSample("alpha", "auth-a", base.Add(time.Duration(i)*time.Second), 100*time.Millisecond, false))
	}
	ins := ComputeInsights(agg.Snapshot(), base.Add(time.Minute))
	if ins.LastAnomaly.Model != "" {
		t.Errorf("expected no anomaly, got %q", ins.LastAnomaly.Model)
	}
}
