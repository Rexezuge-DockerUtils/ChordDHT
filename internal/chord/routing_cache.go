package chord

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

type routingEntry struct {
	targetID      string
	predecessorID string
	successor     NodeInfo
	cachedAt      time.Time
	elem          *list.Element
}

type RoutingCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[string]*routingEntry
	order    *list.List
	hits     atomic.Int64
	misses   atomic.Int64
}

func NewRoutingCache(capacity int, ttl time.Duration) *RoutingCache {
	if capacity <= 0 {
		capacity = DefaultRoutingCacheSize
	}
	if ttl <= 0 {
		ttl = DefaultRoutingCacheTTL
	}
	return &RoutingCache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*routingEntry),
		order:    list.New(),
	}
}

// Get returns the cached successor for targetID if found and not expired.
// It also considers interval caching: a stored entry covers (predecessorID, successor.NodeID].
func (c *RoutingCache) Get(targetID string) (NodeInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[targetID]
	if ok {
		if time.Since(entry.cachedAt) > c.ttl {
			c.evict(entry)
			c.misses.Add(1)
			return NodeInfo{}, false
		}
		c.order.MoveToFront(entry.elem)
		c.hits.Add(1)
		return entry.successor, true
	}

	// Interval lookup: check if any existing entry covers targetID via (predecessorID, successorID]
	for _, e := range c.items {
		if time.Since(e.cachedAt) > c.ttl {
			continue
		}
		if e.predecessorID == "" {
			continue
		}
		if InRangeOpenClosed(targetID, e.predecessorID, e.successor.NodeID) {
			c.order.MoveToFront(e.elem)
			c.hits.Add(1)
			return e.successor, true
		}
	}

	c.misses.Add(1)
	return NodeInfo{}, false
}

// Put stores a routing result for targetID. predecessorID is the predecessor of the successor,
// enabling interval caching for future lookups.
func (c *RoutingCache) Put(targetID, predecessorID string, successor NodeInfo) {
	if targetID == "" || successor.NodeID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.items[targetID]; ok {
		entry.predecessorID = predecessorID
		entry.successor = successor
		entry.cachedAt = time.Now().UTC()
		c.order.MoveToFront(entry.elem)
		return
	}

	if c.order.Len() >= c.capacity {
		c.evictLRU()
	}

	entry := &routingEntry{
		targetID:      targetID,
		predecessorID: predecessorID,
		successor:     successor,
		cachedAt:      time.Now().UTC(),
	}
	entry.elem = c.order.PushFront(entry)
	c.items[targetID] = entry
}

func (c *RoutingCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*routingEntry)
	c.order.Init()
}

func (c *RoutingCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for _, entry := range c.items {
		if now.Sub(entry.cachedAt) > c.ttl {
			c.evict(entry)
		}
	}
}

func (c *RoutingCache) Stats() (hits, misses int64, size int) {
	c.mu.Lock()
	size = len(c.items)
	c.mu.Unlock()
	return c.hits.Load(), c.misses.Load(), size
}

func (c *RoutingCache) evict(entry *routingEntry) {
	c.order.Remove(entry.elem)
	delete(c.items, entry.targetID)
}

func (c *RoutingCache) evictLRU() {
	back := c.order.Back()
	if back == nil {
		return
	}
	entry := back.Value.(*routingEntry)
	c.evict(entry)
}
