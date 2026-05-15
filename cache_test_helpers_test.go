package jpzip

import (
	"context"
	"sync"
)

// memMapCache is a tiny in-process Cache implementation used by tests.
type memMapCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemMapCache() *memMapCache {
	return &memMapCache{data: make(map[string][]byte)}
}

func (c *memMapCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	if !ok {
		return nil, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

func (c *memMapCache) Set(_ context.Context, key string, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	c.data[key] = cp
	return nil
}

func (c *memMapCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}

func (c *memMapCache) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string][]byte)
	return nil
}
