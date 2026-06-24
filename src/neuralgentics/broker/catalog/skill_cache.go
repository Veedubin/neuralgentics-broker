// Package catalog builds and manages server and tool catalogs for the
// Neuralgentics MCP broker. SkillBodyCache provides an in-memory LRU cache
// for SKILL.md body content, used by the broker to avoid repeated disk I/O.
package catalog

import (
	"container/list"
	"os"
	"sync"
	"time"
)

// SkillBodyCache is a thread-safe LRU cache of skill bodies, capped by both
// entry count and total bytes. Entries are invalidated when the underlying
// file's modTime changes (so external skill refreshes auto-invalidate).
type SkillBodyCache struct {
	mu       sync.Mutex
	capacity int                      // max number of skills (e.g., 100)
	maxBytes int64                    // max total bytes (e.g., 5MB)
	ll       *list.List               // doubly-linked list, front=oldest, back=newest
	entries  map[string]*list.Element // key = absolute path
	bytes    int64
	hits     uint64
	misses   uint64
}

type cacheEntry struct {
	path    string
	body    string
	modTime time.Time
	size    int64
}

// NewSkillBodyCache creates a new cache with the given limits.
// capacity=0 means no entry cap; maxBytes=0 means no byte cap. Both = unlimited.
func NewSkillBodyCache(capacity int, maxBytes int64) *SkillBodyCache {
	return &SkillBodyCache{
		capacity: capacity,
		maxBytes: maxBytes,
		ll:       list.New(),
		entries:  make(map[string]*list.Element),
	}
}

// Get returns the cached body for path. If the file's modTime has changed
// since the cache entry was written, the entry is refreshed (file re-read).
// Returns the body and ok=true on hit, "" and ok=false on miss or error.
func (c *SkillBodyCache) Get(path string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[path]; ok {
		entry := elem.Value.(*cacheEntry)
		info, err := os.Stat(path)
		if err != nil {
			// File disappeared — invalidate
			c.removeElement(elem)
			c.misses++
			return "", false
		}
		if !info.ModTime().Equal(entry.modTime) {
			// modTime mismatch — refresh
			body, err := os.ReadFile(path)
			if err != nil {
				c.removeElement(elem)
				c.misses++
				return "", false
			}
			oldSize := entry.size
			entry.body = string(body)
			entry.modTime = info.ModTime()
			entry.size = info.Size()
			c.bytes += info.Size() - oldSize
			c.ll.MoveToBack(elem)
			c.enforceLimits()
			c.hits++
			return entry.body, true
		}
		// Fresh — move to back (most recently used)
		c.ll.MoveToBack(elem)
		c.hits++
		return entry.body, true
	}

	// Miss — read from disk and insert
	info, err := os.Stat(path)
	if err != nil {
		c.misses++
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		c.misses++
		return "", false
	}
	entry := &cacheEntry{
		path:    path,
		body:    string(body),
		modTime: info.ModTime(),
		size:    info.Size(),
	}
	elem := c.ll.PushBack(entry)
	c.entries[path] = elem
	c.bytes += entry.size
	c.enforceLimits()
	c.misses++
	return entry.body, true
}

// Put inserts or refreshes a body for the given path. Used by callers that
// already have the body in memory (e.g., after a fresh Get).
func (c *SkillBodyCache) Put(path, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(path)
	var modTime time.Time
	if err == nil {
		modTime = info.ModTime()
	}
	size := int64(len(body))

	if elem, ok := c.entries[path]; ok {
		entry := elem.Value.(*cacheEntry)
		c.bytes -= entry.size
		entry.body = body
		entry.modTime = modTime
		entry.size = size
		c.bytes += size
		c.ll.MoveToBack(elem)
		c.enforceLimits()
		return
	}

	entry := &cacheEntry{path: path, body: body, modTime: modTime, size: size}
	elem := c.ll.PushBack(entry)
	c.entries[path] = elem
	c.bytes += size
	c.enforceLimits()
}

// Invalidate removes a single entry.
func (c *SkillBodyCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[path]; ok {
		c.removeElement(elem)
	}
}

// InvalidateAll clears the entire cache.
func (c *SkillBodyCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.entries = make(map[string]*list.Element)
	c.bytes = 0
}

// CacheStats holds a snapshot of cache statistics.
type CacheStats struct {
	Entries int    `json:"entries"`
	Bytes   int64  `json:"bytes"`
	Hits    uint64 `json:"hits"`
	Misses  uint64 `json:"misses"`
}

// Stats returns a snapshot of cache statistics.
func (c *SkillBodyCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{
		Entries: c.ll.Len(),
		Bytes:   c.bytes,
		Hits:    c.hits,
		Misses:  c.misses,
	}
}

// removeElement is an internal helper that removes an element and updates
// the bytes counter. Caller must hold the lock.
func (c *SkillBodyCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	c.bytes -= entry.size
	c.ll.Remove(elem)
	delete(c.entries, entry.path)
}

// enforceLimits evicts LRU entries until both capacity and maxBytes are
// satisfied. Caller must hold the lock.
func (c *SkillBodyCache) enforceLimits() {
	for c.capacity > 0 && c.ll.Len() > c.capacity {
		front := c.ll.Front()
		if front == nil {
			return
		}
		c.removeElement(front)
	}
	for c.maxBytes > 0 && c.bytes > c.maxBytes {
		front := c.ll.Front()
		if front == nil {
			return
		}
		c.removeElement(front)
	}
}
