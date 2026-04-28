package caching

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// DistributedCache is a byte-level cache backend for distributed systems.
// Implementations include Redis, Memcached, etc.
type DistributedCache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// DistributedBridge adapts a DistributedCache to a typed Cache[T] using JSON.
type DistributedBridge[T any] struct {
	inner DistributedCache
}

// NewDistributedBridge creates a typed cache bridge over a DistributedCache backend.
func NewDistributedBridge[T any](backend DistributedCache) *DistributedBridge[T] {
	return &DistributedBridge[T]{inner: backend}
}

// Get retrieves and deserializes a value.
func (b *DistributedBridge[T]) Get(ctx context.Context, key string) (T, bool, error) {
	var zero T
	data, found, err := b.inner.Get(ctx, key)
	if err != nil {
		return zero, false, err
	}
	if !found {
		return zero, false, nil
	}
	var val T
	if err := json.Unmarshal(data, &val); err != nil {
		return zero, false, err
	}
	return val, true, nil
}

// Set serializes and stores a value.
func (b *DistributedBridge[T]) Set(ctx context.Context, key string, value T, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.inner.Set(ctx, key, data, ttl)
}

// Invalidate removes a key from the cache.
func (b *DistributedBridge[T]) Invalidate(ctx context.Context, key string) error {
	return b.inner.Delete(ctx, key)
}

// DistributedInMemory is an in-memory implementation of DistributedCache
// suitable for testing and single-process scenarios.
type DistributedInMemory struct {
	mu   sync.RWMutex
	data map[string]distributedEntry
	ttl  time.Duration
}

type distributedEntry struct {
	value  []byte
	expiry time.Time
}

// NewDistributedInMemory creates a new in-memory distributed cache.
func NewDistributedInMemory(opts ...DistributedInMemoryOption) *DistributedInMemory {
	c := &DistributedInMemory{
		data: make(map[string]distributedEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DistributedInMemoryOption configures a DistributedInMemory cache.
type DistributedInMemoryOption func(*DistributedInMemory)

// WithDistributedTTL sets the default TTL for entries.
func WithDistributedTTL(d time.Duration) DistributedInMemoryOption {
	return func(c *DistributedInMemory) { c.ttl = d }
}

// Get implements DistributedCache.Get.
func (m *DistributedInMemory) Get(ctx context.Context, key string) ([]byte, bool, error) {
	m.mu.RLock()
	e, ok := m.data[key]
	m.mu.RUnlock()

	if !ok {
		return nil, false, nil
	}

	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		m.mu.Lock()
		delete(m.data, key)
		m.mu.Unlock()
		return nil, false, nil
	}

	return e.value, true, nil
}

// Set implements DistributedCache.Set.
func (m *DistributedInMemory) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	e := distributedEntry{value: value}
	if ttl > 0 {
		e.expiry = time.Now().Add(ttl)
	} else if m.ttl > 0 {
		e.expiry = time.Now().Add(m.ttl)
	}

	m.mu.Lock()
	m.data[key] = e
	m.mu.Unlock()
	return nil
}

// Delete implements DistributedCache.Delete.
func (m *DistributedInMemory) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}