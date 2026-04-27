package collections

import (
	"sync"
)

// BiMap is a thread-safe bidirectional map.
//
// Example:
//
//	bm := collections.NewBiMap[string, int]()
//	bm.Put("a", 1)
//	v, _ := bm.Get("a")   // 1
//	k, _ := bm.Inverse(1) // "a"
type BiMap[K1 comparable, K2 comparable] struct {
	mu    sync.RWMutex
	forward map[K1]K2
	inverse map[K2]K1
}

// NewBiMap creates a new BiMap.
func NewBiMap[K1 comparable, K2 comparable]() *BiMap[K1, K2] {
	return &BiMap[K1, K2]{
		forward: make(map[K1]K2),
		inverse: make(map[K2]K1),
	}
}

// Put adds a key-value pair, replacing any existing mappings.
func (b *BiMap[K1, K2]) Put(k1 K1, k2 K2) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if oldK2, ok := b.forward[k1]; ok {
		delete(b.inverse, oldK2)
	}
	if oldK1, ok := b.inverse[k2]; ok {
		delete(b.forward, oldK1)
	}
	b.forward[k1] = k2
	b.inverse[k2] = k1
}

// Get returns the value for the given key.
func (b *BiMap[K1, K2]) Get(k1 K1) (K2, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, ok := b.forward[k1]
	return v, ok
}

// Inverse returns the key for the given value.
func (b *BiMap[K1, K2]) Inverse(k2 K2) (K1, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	k, ok := b.inverse[k2]
	return k, ok
}

// Delete removes a mapping by key.
func (b *BiMap[K1, K2]) Delete(k1 K1) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if k2, ok := b.forward[k1]; ok {
		delete(b.inverse, k2)
		delete(b.forward, k1)
	}
}
