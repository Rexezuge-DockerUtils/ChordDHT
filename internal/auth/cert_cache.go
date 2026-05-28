package auth

import (
	"crypto/ed25519"
	"sync"
	"time"
)

type certCacheEntry struct {
	cert     *Certificate
	storedAt time.Time
}

// CertCache stores verified peer certificates keyed by node_id with TOFU semantics.
type CertCache struct {
	mu      sync.RWMutex
	entries map[string]certCacheEntry
	ttl     time.Duration
}

// NewCertCache creates a new CertCache with the given TTL.
func NewCertCache(ttl time.Duration) *CertCache {
	return &CertCache{
		entries: make(map[string]certCacheEntry),
		ttl:     ttl,
	}
}

// Store adds or updates a cert under TOFU rules:
//   - If no entry or TTL-expired: store unconditionally (after CA verification).
//   - If live entry exists: only update if newCert.IssuedAt > existing cert's IssuedAt.
//
// caPublicKey and toleranceSecs are used to re-verify the incoming cert.
func (c *CertCache) Store(cert *Certificate, caPublicKey ed25519.PublicKey, now time.Time, toleranceSecs int) error {
	if err := VerifyCertificate(cert, caPublicKey, now, toleranceSecs); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.entries[cert.NodeID]
	if ok && now.Before(existing.storedAt.Add(c.ttl)) {
		// Live entry: only update if the new cert is newer
		if cert.IssuedAt <= existing.cert.IssuedAt {
			return nil
		}
	}

	c.entries[cert.NodeID] = certCacheEntry{cert: cert, storedAt: now}
	return nil
}

// StoreVerified stores a cert that has already been verified (e.g., extracted from a join body
// that passed middleware verification). Applies TOFU update rules without re-verifying CA sig.
func (c *CertCache) StoreVerified(cert *Certificate, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.entries[cert.NodeID]
	if ok && now.Before(existing.storedAt.Add(c.ttl)) {
		if cert.IssuedAt <= existing.cert.IssuedAt {
			return
		}
	}
	c.entries[cert.NodeID] = certCacheEntry{cert: cert, storedAt: now}
}

// Get retrieves a cert if present and not TTL-expired.
func (c *CertCache) Get(nodeID string, now time.Time) (*Certificate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[nodeID]
	if !ok {
		return nil, false
	}
	if now.After(entry.storedAt.Add(c.ttl)) {
		return nil, false
	}
	return entry.cert, true
}
