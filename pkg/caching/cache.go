// Package caching provides a generic, thread-safe in-memory cache with TTL support.
package caching

import (
	"context"
	"sync"
	"time"
)

// Cache defines the generic caching contract.
//
// Example:
//
//	cache := caching.NewInMemory[string](caching.WithTTL[string](5*time.Minute))
//	cache.Set(ctx, "key", "value", 0)
//	val, ok, err := cache.Get(ctx, "key")
type Cache[T any] interface {
	// Get retrieves a value by key. Returns the value, true if found, and any error.
	Get(ctx context.Context, key string) (T, bool, error)

	// Set stores a value with an optional per-call TTL. If ttl is 0, the default
	// TTL configured on the cache is used.
	Set(ctx context.Context, key string, value T, ttl time.Duration) error

	// Invalidate removes a key from the cache.
	Invalidate(ctx context.Context, key string) error
}

// entry holds a cached value with its expiry time.
type entry[T any] struct {
	value  T
	expiry time.Time
}

// InMemoryCache is a thread-safe in-memory implementation of Cache.
//
// Example:
//
//	cache := caching.NewInMemory[string]()
//	cache.Set(ctx, "greeting", "hello", time.Minute)
type InMemoryCache[T any] struct {
	mu       sync.RWMutex
	data     map[string]entry[T]
	defaultTTL time.Duration
	maxEntries int
}

// InMemoryOption configures an InMemoryCache.
type InMemoryOption[T any] func(*InMemoryCache[T])

// NewInMemory creates a new InMemoryCache with optional configuration.
//
// Example:
//
//	cache := caching.NewInMemory[string](
//	    caching.WithTTL[string](time.Minute),
//	    caching.WithMaxEntries[string](1000),
//	)
func NewInMemory[T any](opts ...InMemoryOption[T]) *InMemoryCache[T] {
	c := &InMemoryCache[T]{
		data: make(map[string]entry[T]),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithTTL sets the default TTL for cache entries.
func WithTTL[T any](d time.Duration) InMemoryOption[T] {
	return func(c *InMemoryCache[T]) { c.defaultTTL = d }
}

// WithMaxEntries sets the maximum number of entries before eviction.
func WithMaxEntries[T any](n int) InMemoryOption[T] {
	return func(c *InMemoryCache[T]) { c.maxEntries = n }
}

// Get implements Cache.Get.
func (c *InMemoryCache[T]) Get(_ context.Context, key string) (T, bool, error) {
	var zero T
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()

	if !ok {
		return zero, false, nil
	}

	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		c.mu.Lock()
		delete(c.data, key)
		c.mu.Unlock()
		return zero, false, nil
	}

	return e.value, true, nil
}

// Set implements Cache.Set.
func (c *InMemoryCache[T]) Set(_ context.Context, key string, value T, ttl time.Duration) error {
	e := entry[T]{value: value}
	if ttl > 0 {
		e.expiry = time.Now().Add(ttl)
	} else if c.defaultTTL > 0 {
		e.expiry = time.Now().Add(c.defaultTTL)
	}

	c.mu.Lock()
	if c.maxEntries > 0 && len(c.data) >= c.maxEntries {
		// Simple eviction: delete one random entry
		for k := range c.data {
			delete(c.data, k)
			break
		}
	}
	c.data[key] = e
	c.mu.Unlock()
	return nil
}

// Invalidate implements Cache.Invalidate.
func (c *InMemoryCache[T]) Invalidate(_ context.Context, key string) error {
	c.mu.Lock()
	delete(c.data, key)
	c.mu.Unlock()
	return nil
}

// Len returns the number of entries currently in the cache.
func (c *InMemoryCache[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}
