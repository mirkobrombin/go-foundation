package collections

import (
	"sync"
)

// MultiMap is a thread-safe map from keys to slices of values.
//
// Example:
//
//	mm := collections.NewMultiMap[string, int]()
//	mm.Add("a", 1)
//	mm.Add("a", 2)
//	vals := mm.Get("a") // []int{1, 2}
type MultiMap[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K][]V
}

// NewMultiMap creates a new MultiMap.
func NewMultiMap[K comparable, V any]() *MultiMap[K, V] {
	return &MultiMap[K, V]{
		data: make(map[K][]V),
	}
}

// Add appends a value to the list for the given key.
func (m *MultiMap[K, V]) Add(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append(m.data[key], value)
}

// Get returns all values for the given key.
func (m *MultiMap[K, V]) Get(key K) []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data[key]
}

// Remove deletes all values for the given key.
func (m *MultiMap[K, V]) Remove(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

// Has checks if a key exists.
func (m *MultiMap[K, V]) Has(key K) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok
}

// Keys returns all keys.
func (m *MultiMap[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]K, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// Len returns the number of keys.
func (m *MultiMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
