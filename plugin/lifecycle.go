package main

import (
	"context"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/John/my-cpa/plugin/aggregator"
	"github.com/John/my-cpa/plugin/config"
	"github.com/John/my-cpa/plugin/persist"
	"github.com/John/my-cpa/plugin/share"
)

type pluginState struct {
	mu     sync.Mutex
	cfg    config.Config
	agg    *aggregator.Aggregator
	store  *persist.Store
	shares *share.Store

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool

	insightsMu    sync.Mutex
	insightsCache []byte
	insightsAt    time.Time
}

var global = &pluginState{}

func (p *pluginState) configure(raw []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, err := config.Load(raw)
	if err != nil {
		return err
	}
	p.cfg = cfg

	if !cfg.Enabled {
		return nil
	}

	if p.started {
		return nil
	}

	p.agg = aggregator.New(cfg.CardinalityLimit, cfg.Retention())

	if cfg.PersistPath != "" {
		p.store = persist.NewStore(cfg.PersistPath)
		if err := p.store.Load(p.agg, cfg.Retention()); err != nil {
			log.Printf("[stats-plugin] snapshot restore failed: %v", err)
		}
	}
	if cfg.ShareEnabled {
		root := cfg.SharePath
		if root == "" && cfg.PersistPath != "" {
			root = filepath.Dir(cfg.PersistPath)
		}
		if root != "" {
			p.shares = share.New(root, cfg.ShareMaxCount, cfg.ShareMaxSnapshot)
			if err := p.shares.Cleanup(); err != nil {
				log.Printf("[stats-plugin] share cleanup failed: %v", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.agg.Start(ctx)

	if p.store != nil {
		p.wg.Add(1)
		go p.persistLoop(ctx)
	}
	if p.shares != nil {
		p.wg.Add(1)
		go p.shareCleanupLoop(ctx)
	}

	p.started = true
	return nil
}

func (p *pluginState) persistLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.PersistInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.store.Save(p.agg); err != nil {
				log.Printf("[stats-plugin] persist failed: %v", err)
			}
		}
	}
}

func (p *pluginState) shareCleanupLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(time.Duration(p.cfg.ShareCleanupSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.shares.Cleanup(); err != nil {
				log.Printf("[stats-plugin] share cleanup failed: %v", err)
			}
		}
	}
}
func (p *pluginState) shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	p.agg.Stop()
	if p.store != nil {
		if err := p.store.Save(p.agg); err != nil {
			log.Printf("[stats-plugin] final persist failed: %v", err)
		}
	}
	p.started = false
}
