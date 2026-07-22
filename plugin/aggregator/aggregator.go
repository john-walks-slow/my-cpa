package aggregator

import (
	"context"
	"sync"
	"time"
)

const queueCapacity = 1024

type Aggregator struct {
	mu       sync.RWMutex
	buckets  map[time.Duration]map[string]*Bucket
	timeline map[time.Duration]map[time.Time]map[string]*Bucket

	queue       chan Sample
	dropCount   uint64
	cardinality int
	retention   time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func New(cardinalityLimit int, retention time.Duration) *Aggregator {
	a := &Aggregator{
		buckets:     make(map[time.Duration]map[string]*Bucket, len(AllWindows)),
		timeline:    make(map[time.Duration]map[time.Time]map[string]*Bucket, len(AllWindows)),
		queue:       make(chan Sample, queueCapacity),
		cardinality: cardinalityLimit,
		retention:   retention,
	}
	for _, w := range AllWindows {
		a.buckets[w] = make(map[string]*Bucket)
		a.timeline[w] = make(map[time.Time]map[string]*Bucket)
	}
	return a
}

func (a *Aggregator) Start(ctx context.Context) {
	ctx, a.cancel = context.WithCancel(ctx)
	a.wg.Add(2)
	go a.ingestLoop(ctx)
	go a.retentionLoop(ctx)
}

func (a *Aggregator) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
}

func (a *Aggregator) Ingest(s Sample) {
	select {
	case a.queue <- s:
	default:
		a.mu.Lock()
		a.dropCount++
		a.mu.Unlock()
	}
}

func (a *Aggregator) DropCount() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.dropCount
}

func (a *Aggregator) ingestLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-a.queue:
			a.ingest(s)
		}
	}
}

func (a *Aggregator) ingest(s Sample) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := s.SeriesKey()
	for _, w := range AllWindows {
		start := s.RequestedAt.Truncate(w)
		m := a.buckets[w]
		b, ok := m[key]
		if !ok {
			if len(m) >= a.cardinality {
				a.evictOldest(m)
			}
			b = NewBucket(w, start)
			m[key] = b
		}
		b.Accumulate(s)
		at := s.RequestedAt.Truncate(w)
		if a.timeline[w][at] == nil {
			a.timeline[w][at] = make(map[string]*Bucket)
		}
		historyBucket := a.timeline[w][at][key]
		if historyBucket == nil {
			historyBucket = NewBucket(w, at)
			a.timeline[w][at][key] = historyBucket
		}
		historyBucket.Accumulate(s)
	}
}

func (a *Aggregator) evictOldest(m map[string]*Bucket) {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, b := range m {
		if first || b.LastSampleAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = b.LastSampleAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(m, oldestKey)
	}
}

func (a *Aggregator) Snapshot() map[time.Duration]map[string]*Bucket {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[time.Duration]map[string]*Bucket, len(a.buckets))
	for w, m := range a.buckets {
		cp := make(map[string]*Bucket, len(m))
		for k, v := range m {
			b := v.Clone()
			cp[k] = b
		}
		out[w] = cp
	}
	return out
}

func (a *Aggregator) Timeline(window time.Duration) map[time.Time]map[string]*Bucket {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[time.Time]map[string]*Bucket, len(a.timeline[window]))
	for at, buckets := range a.timeline[window] {
		copyBuckets := make(map[string]*Bucket, len(buckets))
		for key, bucket := range buckets {
			copyBuckets[key] = bucket.Clone()
		}
		out[at] = copyBuckets
	}
	return out
}

func (a *Aggregator) Restore(data map[time.Duration]map[string]*Bucket) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for w, m := range data {
		if _, ok := a.buckets[w]; !ok {
			a.buckets[w] = make(map[string]*Bucket)
		}
		if _, ok := a.timeline[w]; !ok {
			a.timeline[w] = make(map[time.Time]map[string]*Bucket)
		}
		for k, v := range m {
			a.buckets[w][k] = v
			if a.timeline[w][v.Start] == nil {
				a.timeline[w][v.Start] = make(map[string]*Bucket)
			}
			a.timeline[w][v.Start][k] = v.Clone()
		}
	}
}

func (a *Aggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for w := range a.buckets {
		a.buckets[w] = make(map[string]*Bucket)
		a.timeline[w] = make(map[time.Time]map[string]*Bucket)
	}
}

// IngestDirect synchronously ingests a sample, bypassing the channel.
// Intended for tests and restore paths.
func (a *Aggregator) IngestDirect(s Sample) {
	a.ingest(s)
}
