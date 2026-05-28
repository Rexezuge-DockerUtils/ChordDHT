package auth

import (
	"context"
	"sync"
	"time"
)

// NonceCache prevents replay attacks by tracking seen nonces within a TTL window.
type NonceCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	maxSize int
}

// NewNonceCache creates a new NonceCache. Call StartCleanup to start the background cleaner.
func NewNonceCache(ttl time.Duration, maxSize int) *NonceCache {
	return &NonceCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// Add returns true if the nonce was not previously seen and records it.
// Returns false if the nonce was already seen (replay) or if the cache is full after cleanup.
func (c *NonceCache) Add(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[nonce]; exists {
		return false
	}

	if len(c.entries) >= c.maxSize {
		c.cleanupLocked(now)
		if len(c.entries) >= c.maxSize {
			// Still full after cleanup; reject to prevent memory exhaustion
			return false
		}
	}

	c.entries[nonce] = now.Add(c.ttl)
	return true
}

// Len returns the current number of cached nonces (for monitoring/testing).
func (c *NonceCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// cleanupLocked removes expired entries. Must be called with mu held.
func (c *NonceCache) cleanupLocked(now time.Time) {
	for nonce, exp := range c.entries {
		if now.After(exp) {
			delete(c.entries, nonce)
		}
	}
}

// StartCleanup launches a background goroutine that removes expired nonces every 5 minutes.
func (c *NonceCache) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				c.mu.Lock()
				c.cleanupLocked(t)
				c.mu.Unlock()
			}
		}
	}()
}
