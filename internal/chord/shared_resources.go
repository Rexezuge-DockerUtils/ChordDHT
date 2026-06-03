package chord

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// NodeInfoCache is an L0 (physically shared) TTL+LRU cache for full NodeInfo objects.
// Finger tables store only node_id strings (lightweight); full NodeInfo is fetched here.
type NodeInfoCache struct {
	mu       sync.RWMutex
	size     int
	ttl      time.Duration
	items    map[string]*nodeInfoEntry
	order    *list.List
}

type nodeInfoEntry struct {
	nodeID   string
	info     NodeInfo
	cachedAt time.Time
	elem     *list.Element
}

func NewNodeInfoCache(size int, ttl time.Duration) *NodeInfoCache {
	if size <= 0 {
		size = 2048
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &NodeInfoCache{
		size:  size,
		ttl:   ttl,
		items: make(map[string]*nodeInfoEntry),
		order: list.New(),
	}
}

func (c *NodeInfoCache) Put(info NodeInfo) {
	if info.NodeID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[info.NodeID]; ok {
		e.info = info
		e.cachedAt = time.Now().UTC()
		c.order.MoveToFront(e.elem)
		return
	}
	if c.order.Len() >= c.size {
		back := c.order.Back()
		if back != nil {
			old := back.Value.(*nodeInfoEntry)
			c.order.Remove(back)
			delete(c.items, old.nodeID)
		}
	}
	entry := &nodeInfoEntry{nodeID: info.NodeID, info: info, cachedAt: time.Now().UTC()}
	entry.elem = c.order.PushFront(entry)
	c.items[info.NodeID] = entry
}

func (c *NodeInfoCache) Get(nodeID string) (NodeInfo, bool) {
	c.mu.RLock()
	e, ok := c.items[nodeID]
	if !ok {
		c.mu.RUnlock()
		return NodeInfo{}, false
	}
	expired := time.Since(e.cachedAt) > c.ttl
	info := e.info
	c.mu.RUnlock()
	if expired {
		c.mu.Lock()
		if e2, ok2 := c.items[nodeID]; ok2 && time.Since(e2.cachedAt) > c.ttl {
			c.order.Remove(e2.elem)
			delete(c.items, nodeID)
		}
		c.mu.Unlock()
		return NodeInfo{}, false
	}
	return info, true
}

// ProofVerifyCache is an L0 cache for VNodeProof Ed25519 verification results.
// The key is SHA1(vnode_id + anchor_id + expires_at), binding to a specific proof version.
type ProofVerifyCache struct {
	mu    sync.Mutex
	size  int
	items map[string]bool
	order *list.List
}

type proofVerifyEntry struct {
	key   string
	valid bool
	elem  *list.Element
}

func NewProofVerifyCache(size int) *ProofVerifyCache {
	if size <= 0 {
		size = 256
	}
	return &ProofVerifyCache{
		size:  size,
		items: make(map[string]bool),
		order: list.New(),
	}
}

func proofCacheKey(vnodeID, anchorID string, expiresAt int64) string {
	raw := fmt.Sprintf("%s%s%d", vnodeID, anchorID, expiresAt)
	h := sha1.Sum([]byte(raw))
	return hex.EncodeToString(h[:])
}

// Get returns (valid, found). If found is false, no cached result exists.
func (c *ProofVerifyCache) Get(vnodeID, anchorID string, expiresAt int64) (valid, found bool) {
	key := proofCacheKey(vnodeID, anchorID, expiresAt)
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[key]
	return v, ok
}

// Put stores the verification result for a proof.
func (c *ProofVerifyCache) Put(vnodeID, anchorID string, expiresAt int64, valid bool) {
	key := proofCacheKey(vnodeID, anchorID, expiresAt)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	if c.order.Len() >= c.size {
		back := c.order.Back()
		if back != nil {
			old := back.Value.(*proofVerifyEntry)
			c.order.Remove(back)
			delete(c.items, old.key)
		}
	}
	entry := &proofVerifyEntry{key: key, valid: valid}
	entry.elem = c.order.PushFront(entry)
	c.items[key] = valid
}

// Invalidate clears all cached results (called on CRL refresh or key rotation).
func (c *ProofVerifyCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]bool)
	c.order.Init()
}
