package trace

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// newTestCache 返回带可控时钟的缓存。
func newTestCache(t *testing.T, ttl time.Duration) (*Cache[string], *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	cache := NewCache[string](ttl)
	cache.now = func() time.Time { return now }
	return cache, &now
}

// TestCacheHitAndExpire 覆盖命中与过期失效。
func TestCacheHitAndExpire(t *testing.T) {
	cache, now := newTestCache(t, time.Minute)
	cache.Put("s1:10", "分析结果")

	if got, ok := cache.Get("s1:10"); !ok || got != "分析结果" {
		t.Fatalf("命中失败: %q, %v", got, ok)
	}
	// 未到 TTL:仍命中。
	*now = now.Add(59 * time.Second)
	if _, ok := cache.Get("s1:10"); !ok {
		t.Fatal("TTL 内不应失效")
	}
	// 超过 TTL:失效。
	*now = now.Add(2 * time.Second)
	if got, ok := cache.Get("s1:10"); ok {
		t.Fatalf("过期后仍命中: %q", got)
	}
	if cache.Len() != 0 {
		t.Errorf("过期条目未被清理: Len = %d", cache.Len())
	}
}

// TestCacheKeysAreIndependent 覆盖多会话互不干扰。
func TestCacheKeysAreIndependent(t *testing.T) {
	cache, _ := newTestCache(t, time.Minute)
	cache.Put("s1:10", "一")
	cache.Put("s2:3", "二")
	if got, ok := cache.Get("s1:10"); !ok || got != "一" {
		t.Errorf("s1 取值 = %q, %v", got, ok)
	}
	if got, ok := cache.Get("s2:3"); !ok || got != "二" {
		t.Errorf("s2 取值 = %q, %v", got, ok)
	}
	if _, ok := cache.Get("s3:1"); ok {
		t.Error("未写入的键命中")
	}
}

// TestCacheNewTurnInvalidatesByKey 覆盖键含最后一轮 request_id 的失效约定:
// 新轮次产生新键,旧键的结果不再被使用。
func TestCacheNewTurnInvalidatesByKey(t *testing.T) {
	cache, _ := newTestCache(t, time.Minute)
	cache.Put("s1:10", "旧结果")
	cache.Put("s1:12", "新结果")
	if got, _ := cache.Get("s1:12"); got != "新结果" {
		t.Errorf("新键取值 = %q", got)
	}
	// 旧键仍在缓存中(未被访问),但调用方不会再按它查询——键即版本。
	if got, ok := cache.Get("s1:10"); !ok || got != "旧结果" {
		t.Errorf("旧键 = %q, %v", got, ok)
	}
}

// TestCacheEviction 覆盖容量控制:超限时清过期项,仍超限则淘汰最早条目。
func TestCacheEviction(t *testing.T) {
	cache, now := newTestCache(t, time.Minute)
	// 先填满并让最早若干条过期。
	for i := range cacheMaxEntries {
		cache.Put("old:"+strconv.Itoa(i), "v")
		*now = now.Add(time.Second)
	}
	*now = now.Add(2 * time.Minute) // 全部过期
	// 再写入:触发清理,过期项应被清掉。
	cache.Put("fresh", "v")
	if cache.Len() != 1 {
		t.Fatalf("清理后条目数 = %d, want 1", cache.Len())
	}

	// 容量上限:持续写入后条目数不超过上限。
	cache2, _ := newTestCache(t, time.Hour)
	for i := range cacheMaxEntries + 10 {
		cache2.Put("k:"+strconv.Itoa(i), "v")
	}
	if got := cache2.Len(); got > cacheMaxEntries {
		t.Errorf("条目数 = %d, 超过上限 %d", got, cacheMaxEntries)
	}
}

// TestCacheConcurrent 覆盖并发读写(配合 -race 运行)。
func TestCacheConcurrent(t *testing.T) {
	cache := NewCache[int](time.Minute)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "s:" + strconv.Itoa(n)
			for j := range 50 {
				cache.Put(key, j)
				cache.Get(key)
			}
		}(i)
	}
	wg.Wait()
	if cache.Len() > cacheMaxEntries {
		t.Errorf("并发写入后条目数 = %d", cache.Len())
	}
}

// TestNewCacheDefaultTTL 覆盖 TTL 默认值。
func TestNewCacheDefaultTTL(t *testing.T) {
	cache := NewCache[int](0)
	if cache.ttl != CacheDefaultTTL {
		t.Errorf("默认 TTL = %v, want %v", cache.ttl, CacheDefaultTTL)
	}
}
