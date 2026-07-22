package config

import (
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Enabled {
		t.Error("default enabled should be true")
	}
	if cfg.RetentionMinutes != 1440 {
		t.Errorf("retention = %d, want 1440", cfg.RetentionMinutes)
	}
	if cfg.PersistIntervalSec != 30 {
		t.Errorf("persist_interval = %d, want 30", cfg.PersistIntervalSec)
	}
	if cfg.CardinalityLimit != 50000 {
		t.Errorf("cardinality = %d, want 50000", cfg.CardinalityLimit)
	}
	if cfg.PersistPath != "" {
		t.Errorf("persist_path = %q, want empty", cfg.PersistPath)
	}
}

func TestLoadEmpty(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("load nil: %v", err)
	}
	if cfg.RetentionMinutes != 1440 {
		t.Errorf("retention = %d, want 1440", cfg.RetentionMinutes)
	}
}

func TestLoadValid(t *testing.T) {
	raw := []byte(`
enabled: true
retention_minutes: 60
persist_path: "data/stats.json"
persist_interval_sec: 10
cardinality_limit: 1000
`)
	cfg, err := Load(raw)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RetentionMinutes != 60 {
		t.Errorf("retention = %d, want 60", cfg.RetentionMinutes)
	}
	if cfg.PersistPath != "data/stats.json" {
		t.Errorf("persist_path = %q", cfg.PersistPath)
	}
	if cfg.PersistIntervalSec != 10 {
		t.Errorf("persist_interval = %d, want 10", cfg.PersistIntervalSec)
	}
	if cfg.CardinalityLimit != 1000 {
		t.Errorf("cardinality = %d, want 1000", cfg.CardinalityLimit)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	_, err := Load([]byte(":::invalid"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadNegativeValuesFallback(t *testing.T) {
	raw := []byte(`
retention_minutes: -5
persist_interval_sec: 0
cardinality_limit: -100
`)
	cfg, err := Load(raw)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RetentionMinutes != 1440 {
		t.Errorf("retention = %d, want default 1440", cfg.RetentionMinutes)
	}
	if cfg.PersistIntervalSec != 30 {
		t.Errorf("persist_interval = %d, want default 30", cfg.PersistIntervalSec)
	}
	if cfg.CardinalityLimit != 50000 {
		t.Errorf("cardinality = %d, want default 50000", cfg.CardinalityLimit)
	}
}

func TestRetentionDuration(t *testing.T) {
	cfg := Config{RetentionMinutes: 120}
	if cfg.Retention().Minutes() != 120 {
		t.Errorf("Retention() = %v, want 120m", cfg.Retention())
	}
}

func TestPersistIntervalDuration(t *testing.T) {
	cfg := Config{PersistIntervalSec: 15}
	if cfg.PersistInterval().Seconds() != 15 {
		t.Errorf("PersistInterval() = %v, want 15s", cfg.PersistInterval())
	}
}
