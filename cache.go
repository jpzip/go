package jpzip

import (
	"container/list"
	"context"
	"sync"
)

// Cache is the abstract interface a user-supplied L2 persistent cache must
// satisfy. Implementations are free to add TTLs, eviction, or backends.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
}

// memoryLRU is the L1 in-memory cache, bounded by a fixed number of prefix
// entries. It is safe for concurrent use.
type memoryLRU struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

type lruEntry struct {
	key   string
	value ZipcodeDict
}

func newMemoryLRU(capacity int) *memoryLRU {
	if capacity < 1 {
		capacity = 1
	}
	return &memoryLRU{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

func (c *memoryLRU) get(key string) (ZipcodeDict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruEntry).value, true
}

func (c *memoryLRU) set(key string, value ZipcodeDict) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).value = value
		c.ll.MoveToFront(el)
		return
	}
	if c.ll.Len() >= c.capacity {
		oldest := c.ll.Back()
		if oldest != nil {
			delete(c.items, oldest.Value.(*lruEntry).key)
			c.ll.Remove(oldest)
		}
	}
	el := c.ll.PushFront(&lruEntry{key: key, value: value})
	c.items[key] = el
}

func (c *memoryLRU) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element, c.capacity)
}

func (c *memoryLRU) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
