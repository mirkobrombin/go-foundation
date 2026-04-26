package safemap

import (
	"hash/fnv"
	"math/bits"
)

// Hasher is a function that returns a hash for a key.
type Hasher[K any] func(K) uint64

// ShardedMap is a thread-safe map partitioned into multiple shards to reduce lock contention.
type ShardedMap[K comparable, V any] struct {
	shards []*Map[K, V]
	mask   uint64
	hasher Hasher[K]
}

// NewSharded creates a new ShardedMap.
//
// shardCount must be a power of 2. If not, it will be rounded up to the next power of 2.
// Default shardCount is 32 if input <= 0.
func NewSharded[K comparable, V any](hasher Hasher[K], shardCount int) *ShardedMap[K, V] {
	if shardCount <= 0 {
		shardCount = 32
	}
	// Ensure power of 2
	if bits.OnesCount(uint(shardCount)) != 1 {
		shardCount = 1 << bits.Len(uint(shardCount))
	}

	shards := make([]*Map[K, V], shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = New[K, V]()
	}

	return &ShardedMap[K, V]{
		shards: shards,
		mask:   uint64(shardCount - 1),
		hasher: hasher,
	}
}

// getShard returns the shard for the given key.
func (m *ShardedMap[K, V]) getShard(key K) *Map[K, V] {
	hash := m.hasher(key)
	return m.shards[hash&m.mask]
}

// Set stores a value by key.
func (m *ShardedMap[K, V]) Set(key K, value V) {
	m.getShard(key).Set(key, value)
}

// Get retrieves a value by key.
func (m *ShardedMap[K, V]) Get(key K) (V, bool) {
	return m.getShard(key).Get(key)
}

// Delete removes a key.
func (m *ShardedMap[K, V]) Delete(key K) {
	m.getShard(key).Delete(key)
}

// Has checks if a key exists.
func (m *ShardedMap[K, V]) Has(key K) bool {
	return m.getShard(key).Has(key)
}

// Len returns the total number of items across all shards.
func (m *ShardedMap[K, V]) Len() int {
	total := 0
	for _, shard := range m.shards {
		total += shard.Len()
	}
	return total
}

// Clear removes all items from all shards.
func (m *ShardedMap[K, V]) Clear() {
	for _, shard := range m.shards {
		shard.Clear()
	}
}

// Range iterates over all items in all shards.
// Iteration order is random between shards.
func (m *ShardedMap[K, V]) Range(fn func(key K, value V) bool) {
	for _, shard := range m.shards {
		cont := true
		shard.Range(func(k K, v V) bool {
			if !fn(k, v) {
				cont = false
				return false
			}
			return true
		})
		if !cont {
			break
		}
	}
}

// GetOrSet returns the value or sets a default.
func (m *ShardedMap[K, V]) GetOrSet(key K, defaultValue V) V {
	return m.getShard(key).GetOrSet(key, defaultValue)
}

// Compute atomically updates a value using a function.
func (m *ShardedMap[K, V]) Compute(key K, fn func(existing V, exists bool) V) V {
	return m.getShard(key).Compute(key, fn)
}

// Keys returns all keys across all shards.
func (m *ShardedMap[K, V]) Keys() []K {
	var all []K
	for _, shard := range m.shards {
		all = append(all, shard.Keys()...)
	}
	return all
}

// Values returns all values across all shards.
func (m *ShardedMap[K, V]) Values() []V {
	var all []V
	for _, shard := range m.shards {
		all = append(all, shard.Values()...)
	}
	return all
}

// StringHasher returns a hasher for string keys.
func StringHasher(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
