package trace

import (
	"sync"
	"time"
)

// CacheDefaultTTL 是会话级推断结果的默认缓存时长(design.md D5)。
const CacheDefaultTTL = 60 * time.Second

// cacheMaxEntries 是缓存条目上限:超过时先清过期项,再淘汰最早的一条。
// 会话数量会随使用增长,有上限才不至于把内存吃满。
const cacheMaxEntries = 64

// cacheEntry 是缓存条目。
type cacheEntry[V any] struct {
	value    V
	storedAt time.Time
}

// Cache 是带 TTL 的进程内键值缓存,用于复用会话级推断结果。
// 使用约定:键应包含「该会话最后一轮的 request_id」——有新轮次时键自然变化,
// 旧结果随之失效,无需显式失效逻辑(design.md D5)。
type Cache[V any] struct {
	ttl time.Duration
	// now 可注入,便于测试过期行为。
	now func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry[V]
}

// NewCache 创建 TTL 缓存;ttl ≤ 0 时取 CacheDefaultTTL。
func NewCache[V any](ttl time.Duration) *Cache[V] {
	if ttl <= 0 {
		ttl = CacheDefaultTTL
	}
	return &Cache[V]{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]cacheEntry[V]),
	}
}

// Get 取值:未命中或已过期时返回零值与 false,过期条目顺带清理。
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	if c.expired(entry) {
		delete(c.entries, key)
		var zero V
		return zero, false
	}
	return entry.value, true
}

// Put 写入条目并做容量控制。
func (c *Cache[V]) Put(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry[V]{value: value, storedAt: c.now()}
	if len(c.entries) > cacheMaxEntries {
		c.evictLocked()
	}
}

// Len 返回当前条目数(含尚未清理的过期项)。
func (c *Cache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// expired 判断条目是否已过期。
func (c *Cache[V]) expired(entry cacheEntry[V]) bool {
	return c.now().Sub(entry.storedAt) > c.ttl
}

// evictLocked 清理过期项;仍超限时淘汰最早写入的一条。
func (c *Cache[V]) evictLocked() {
	for key, entry := range c.entries {
		if c.expired(entry) {
			delete(c.entries, key)
		}
	}
	if len(c.entries) <= cacheMaxEntries {
		return
	}
	oldestKey := ""
	var oldestAt time.Time
	for key, entry := range c.entries {
		if oldestKey == "" || entry.storedAt.Before(oldestAt) {
			oldestKey, oldestAt = key, entry.storedAt
		}
	}
	delete(c.entries, oldestKey)
}
