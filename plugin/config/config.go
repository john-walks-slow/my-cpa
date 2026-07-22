package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Enabled            bool   `yaml:"enabled"`
	RetentionMinutes   int    `yaml:"retention_minutes"`
	PersistPath        string `yaml:"persist_path"`
	PersistIntervalSec int    `yaml:"persist_interval_sec"`
	CardinalityLimit   int    `yaml:"cardinality_limit"`
	DashboardEnabled   *bool  `yaml:"dashboard_enabled"`
	ShareEnabled       bool   `yaml:"share_enabled"`
	SharePath          string `yaml:"share_path"`
	ShareMaxCount      int    `yaml:"share_max_count"`
	ShareCleanupSec    int    `yaml:"share_cleanup_interval_sec"`
	ShareMaxSnapshot   int64  `yaml:"share_max_snapshot_bytes"`
}

func (c Config) IsDashboardEnabled() bool {
	if c.DashboardEnabled == nil {
		return true
	}
	return *c.DashboardEnabled
}

func (c Config) Retention() time.Duration {
	return time.Duration(c.RetentionMinutes) * time.Minute
}

func (c Config) PersistInterval() time.Duration {
	return time.Duration(c.PersistIntervalSec) * time.Second
}

func Default() Config {
	return Config{
		Enabled:            true,
		RetentionMinutes:   1440,
		PersistPath:        "",
		PersistIntervalSec: 30,
		CardinalityLimit:   50000,
		ShareEnabled:       true,
		ShareMaxCount:      1000,
		ShareCleanupSec:    3600,
		ShareMaxSnapshot:   5 * 1024 * 1024,
	}
}

func Load(raw []byte) (Config, error) {
	cfg := Default()
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.RetentionMinutes <= 0 {
		cfg.RetentionMinutes = Default().RetentionMinutes
	}
	if cfg.PersistIntervalSec <= 0 {
		cfg.PersistIntervalSec = Default().PersistIntervalSec
	}
	if cfg.CardinalityLimit <= 0 {
		cfg.CardinalityLimit = Default().CardinalityLimit
	}
	if cfg.ShareMaxCount < 0 {
		cfg.ShareMaxCount = Default().ShareMaxCount
	}
	if cfg.ShareCleanupSec <= 0 {
		cfg.ShareCleanupSec = Default().ShareCleanupSec
	}
	if cfg.ShareMaxSnapshot <= 0 {
		cfg.ShareMaxSnapshot = Default().ShareMaxSnapshot
	}
	return cfg, nil
}
