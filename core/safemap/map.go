package safemap

import "sync"

// Map is a generic thread-safe map.
//
// It uses a sync.RWMutex to guard access to the underlying map.
//
// Example:
//
//	m := safemap.New[string, int]()
//	m.Set("key", 42)
type Map[K comparable, V any] struct {
	data map[K]V
	mu   sync.RWMutex
}

// New creates a new empty SafeMap.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		data: make(map[K]V),
	}
}

// Get retrieves a value by key.
//
// Returns:
//
// The value and true if found, otherwise zero value and false.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

// Set stores a value by key.
func (m *Map[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
}

// Delete removes a key from the map.
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

// Has checks if a key exists.
func (m *Map[K, V]) Has(key K) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Keys returns all keys in the map.
//
// Notes:
//
// The order of keys is random.
func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]K, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values in the map.
//
// Notes:
//
// The order of values corresponds to the iteration order of the map.
func (m *Map[K, V]) Values() []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vals := make([]V, 0, len(m.data))
	for _, v := range m.data {
		vals = append(vals, v)
	}
	return vals
}

// Clear removes all items from the map.
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[K]V)
}

// Range iterates over all key-value pairs.
//
// If fn returns false, iteration stops.
//
// Example:
//
//	m.Range(func(k string, v int) bool {
//		fmt.Println(k, v)
//		return true
//	})
func (m *Map[K, V]) Range(fn func(key K, value V) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		if !fn(k, v) {
			break
		}
	}
}

// GetOrSet returns the value for key if present, otherwise sets and returns the default.
func (m *Map[K, V]) GetOrSet(key K, defaultValue V) V {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.data[key]; ok {
		return v
	}
	m.data[key] = defaultValue
	return defaultValue
}

// Compute atomically updates a value using a function.
//
// Example:
//
//	m.Compute("counter", func(v int, exists bool) int {
//		return v + 1
//	})
func (m *Map[K, V]) Compute(key K, fn func(existing V, exists bool) V) V {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, exists := m.data[key]
	newVal := fn(existing, exists)
	m.data[key] = newVal
	return newVal
}
