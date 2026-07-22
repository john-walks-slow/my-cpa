package aggregator

import (
	"context"
	"time"
)

func (a *Aggregator) retentionLoop(ctx context.Context) {
	defer a.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.evictExpired()
		}
	}
}

func (a *Aggregator) evictExpired() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for w, buckets := range a.buckets {
		for key, bucket := range buckets {
			if now.Sub(bucket.Start) > a.retention+bucket.Window {
				delete(buckets, key)
			}
		}
		for at := range a.timeline[w] {
			if now.Sub(at) > a.retention+w {
				delete(a.timeline[w], at)
			}
		}
	}
}
