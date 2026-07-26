package safemap

import (
	"hash/fnv"
	"math/bits"
	"sync"
	"time"
)

// Hasher computes a uint64 hash for keys.
type Hasher[K any] func(K) uint64

type ttlEntry[V any] struct {
	value  V
	expiry time.Time
}

func (e ttlEntry[V]) expired() bool {
	return !e.expiry.IsZero() && time.Now().After(e.expiry)
}

// ShardedMap is a concurrent map with shard-level locking and TTL support.
type ShardedMap[K comparable, V any] struct {
	shards []*concurrentShard[K, V]
	mask   uint64
	hasher Hasher[K]
	expiry time.Duration
	mu     sync.Mutex
}

type concurrentShard[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]ttlEntry[V]
}

// NewSharded creates a new ShardedMap with the given hasher and shard count.
func NewSharded[K comparable, V any](hasher Hasher[K], shardCount int) *ShardedMap[K, V] {
	if hasher == nil {
		panic("safemap: hasher cannot be nil")
	}
	if shardCount <= 0 {
		shardCount = 32
	}
	if bits.OnesCount(uint(shardCount)) != 1 {
		shardCount = 1 << bits.Len(uint(shardCount))
	}

	shards := make([]*concurrentShard[K, V], shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &concurrentShard[K, V]{data: make(map[K]ttlEntry[V])}
	}

	return &ShardedMap[K, V]{
		shards: shards,
		mask:   uint64(shardCount - 1),
		hasher: hasher,
	}
}

// WithExpiry sets the default TTL for entries.
func (m *ShardedMap[K, V]) WithExpiry(d time.Duration) *ShardedMap[K, V] {
	m.mu.Lock()
	m.expiry = d
	m.mu.Unlock()
	return m
}

func (m *ShardedMap[K, V]) expiryDuration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expiry
}

func (m *ShardedMap[K, V]) getShard(key K) *concurrentShard[K, V] {
	hash := m.hasher(key)
	return m.shards[hash&m.mask]
}

// Set stores a key-value pair.
func (m *ShardedMap[K, V]) Set(key K, value V) {
	s := m.getShard(key)
	e := ttlEntry[V]{value: value}
	if expiry := m.expiryDuration(); expiry > 0 {
		e.expiry = time.Now().Add(expiry)
	}
	s.mu.Lock()
	s.data[key] = e
	s.mu.Unlock()
}

// Get retrieves a value by key.
func (m *ShardedMap[K, V]) Get(key K) (V, bool) {
	s := m.getShard(key)
	s.mu.RLock()
	e, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		var zero V
		return zero, false
	}
	if e.expired() {
		s.mu.Lock()
		current, exists := s.data[key]
		if exists && current.expired() {
			delete(s.data, key)
			exists = false
		}
		s.mu.Unlock()
		if exists {
			return current.value, true
		}
		var zero V
		return zero, false
	}
	return e.value, true
}

// Delete removes a key from the map.
func (m *ShardedMap[K, V]) Delete(key K) {
	shard := m.getShard(key)
	shard.mu.Lock()
	delete(shard.data, key)
	shard.mu.Unlock()
}

// Has reports whether a key exists.
func (m *ShardedMap[K, V]) Has(key K) bool {
	_, ok := m.Get(key)
	return ok
}

// Len returns the total number of entries.
func (m *ShardedMap[K, V]) Len() int {
	total := 0
	for _, s := range m.shards {
		s.mu.Lock()
		for key, entry := range s.data {
			if entry.expired() {
				delete(s.data, key)
				continue
			}
			total++
		}
		s.mu.Unlock()
	}
	return total
}

// Clear removes all entries.
func (m *ShardedMap[K, V]) Clear() {
	for _, s := range m.shards {
		s.mu.Lock()
		s.data = make(map[K]ttlEntry[V])
		s.mu.Unlock()
	}
}

// Range iterates over non-expired entries until fn returns false.
func (m *ShardedMap[K, V]) Range(fn func(key K, value V) bool) {
	type pair struct {
		key   K
		value V
	}
	var entries []pair
	for _, s := range m.shards {
		s.mu.Lock()
		for k, e := range s.data {
			if e.expired() {
				delete(s.data, k)
				continue
			}
			entries = append(entries, pair{key: k, value: e.value})
		}
		s.mu.Unlock()
	}
	for _, entry := range entries {
		if !fn(entry.key, entry.value) {
			return
		}
	}
}

// GetOrSet returns the existing value or sets and returns the default.
func (m *ShardedMap[K, V]) GetOrSet(key K, defaultValue V) V {
	s := m.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.data[key]; ok && !e.expired() {
		return e.value
	}
	e := ttlEntry[V]{value: defaultValue}
	if expiry := m.expiryDuration(); expiry > 0 {
		e.expiry = time.Now().Add(expiry)
	}
	s.data[key] = e
	return defaultValue
}

// Compute atomically updates or inserts a value.
func (m *ShardedMap[K, V]) Compute(key K, fn func(existing V, exists bool) V) V {
	s := m.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if ok && e.expired() {
		delete(s.data, key)
		ok = false
	}
	var newVal V
	if ok {
		newVal = fn(e.value, true)
	} else {
		var zero V
		newVal = fn(zero, false)
	}
	ne := ttlEntry[V]{value: newVal}
	if expiry := m.expiryDuration(); expiry > 0 {
		ne.expiry = time.Now().Add(expiry)
	}
	s.data[key] = ne
	return newVal
}

// Keys returns all non-expired keys.
func (m *ShardedMap[K, V]) Keys() []K {
	var all []K
	for _, s := range m.shards {
		s.mu.Lock()
		for k, e := range s.data {
			if e.expired() {
				delete(s.data, k)
				continue
			}
			all = append(all, k)
		}
		s.mu.Unlock()
	}
	return all
}

// Values returns all non-expired values.
func (m *ShardedMap[K, V]) Values() []V {
	var all []V
	for _, s := range m.shards {
		s.mu.Lock()
		for key, e := range s.data {
			if e.expired() {
				delete(s.data, key)
				continue
			}
			all = append(all, e.value)
		}
		s.mu.Unlock()
	}
	return all
}

// StringHasher is a Hasher[string] using FNV-1a 64-bit.
func StringHasher(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
