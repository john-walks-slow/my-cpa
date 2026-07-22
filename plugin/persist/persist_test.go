package persist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	store := NewStore(path)

	agg := aggregator.New(1000, time.Hour)
	agg.IngestDirect(aggregator.Sample{
		Provider:     "openai",
		Model:        "gpt-5.5",
		Alias:        "gpt-5.5",
		AuthID:       "auth-1",
		RequestedAt:  time.Now(),
		Latency:      200 * time.Millisecond,
		TTFT:         50 * time.Millisecond,
		OutputTokens: 100,
		InputTokens:  50,
	})

	if err := store.Save(agg); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot file not created: %v", err)
	}

	agg2 := aggregator.New(1000, time.Hour)
	if err := store.Load(agg2, time.Hour); err != nil {
		t.Fatalf("load: %v", err)
	}

	snap := agg2.Snapshot()
	var found bool
	for _, m := range snap {
		for _, b := range m {
			if b.Count > 0 {
				found = true
				if b.SumOutput != 100 {
					t.Errorf("sum_output = %d, want 100", b.SumOutput)
				}
			}
		}
	}
	if !found {
		t.Error("no data restored from snapshot")
	}
}

func TestLoadCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	store := NewStore(path)
	agg := aggregator.New(1000, time.Hour)
	err := store.Load(agg, time.Hour)
	if err == nil {
		t.Error("expected error for corrupted file")
	}
}

func TestLoadMissingFile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent.json"))
	agg := aggregator.New(1000, time.Hour)
	if err := store.Load(agg, time.Hour); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
}

func TestLoadExpiredSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	store := NewStore(path)

	agg := aggregator.New(1000, time.Hour)
	agg.IngestDirect(aggregator.Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-1",
		RequestedAt: time.Now(),
		Latency:     time.Millisecond,
	})
	store.Save(agg)

	agg2 := aggregator.New(1000, time.Hour)
	err := store.Load(agg2, time.Nanosecond)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	snap := agg2.Snapshot()
	for _, m := range snap {
		if len(m) != 0 {
			t.Error("expired snapshot should restore nothing")
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	store := NewStore(path)

	agg := aggregator.New(1000, time.Hour)
	agg.IngestDirect(aggregator.Sample{
		Provider:    "openai",
		Model:       "gpt-5.5",
		AuthID:      "auth-1",
		RequestedAt: time.Now(),
		Latency:     time.Millisecond,
	})

	for i := 0; i < 5; i++ {
		if err := store.Save(agg); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful save")
	}
}
