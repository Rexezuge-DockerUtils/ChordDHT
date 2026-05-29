package chord

import (
	"sync"
	"time"
)

type rttSample struct {
	ewmaMs    float64
	updatedAt time.Time
}

type RTTCache struct {
	mu      sync.RWMutex
	samples map[string]*rttSample
	alpha   float64
	expiry  time.Duration
}

func NewRTTCache(alpha float64, expiry time.Duration) *RTTCache {
	if alpha <= 0 || alpha > 1 {
		alpha = DefaultRTTEWMAAlpha
	}
	if expiry <= 0 {
		expiry = DefaultRTTSampleExpiry
	}
	return &RTTCache{
		samples: make(map[string]*rttSample),
		alpha:   alpha,
		expiry:  expiry,
	}
}

func (c *RTTCache) Record(nodeID string, rttMs int64) {
	if nodeID == "" || rttMs < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.samples[nodeID]; ok {
		s.ewmaMs = c.alpha*float64(rttMs) + (1-c.alpha)*s.ewmaMs
		s.updatedAt = time.Now().UTC()
	} else {
		c.samples[nodeID] = &rttSample{
			ewmaMs:    float64(rttMs),
			updatedAt: time.Now().UTC(),
		}
	}
}

func (c *RTTCache) Get(nodeID string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.samples[nodeID]
	if !ok {
		return 0, false
	}
	if time.Since(s.updatedAt) > c.expiry {
		return 0, false
	}
	return int64(s.ewmaMs), true
}

func (c *RTTCache) Snapshot() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int64, len(c.samples))
	now := time.Now().UTC()
	for id, s := range c.samples {
		if now.Sub(s.updatedAt) <= c.expiry {
			out[id] = int64(s.ewmaMs)
		}
	}
	return out
}

func (c *RTTCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for id, s := range c.samples {
		if now.Sub(s.updatedAt) > c.expiry {
			delete(c.samples, id)
		}
	}
}
