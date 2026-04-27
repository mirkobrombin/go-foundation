package safemap

import (
	"hash/fnv"
	"math/bits"
	"sync"
	"time"
)

type Hasher[K any] func(K) uint64

type ttlEntry[V any] struct {
	value  V
	expiry time.Time
}

func (e ttlEntry[V]) expired() bool {
	return !e.expiry.IsZero() && time.Now().After(e.expiry)
}

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

func NewSharded[K comparable, V any](hasher Hasher[K], shardCount int) *ShardedMap[K, V] {
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

func (m *ShardedMap[K, V]) WithExpiry(d time.Duration) *ShardedMap[K, V] {
	m.expiry = d
	return m
}

func (m *ShardedMap[K, V]) getShard(key K) *concurrentShard[K, V] {
	hash := m.hasher(key)
	return m.shards[hash&m.mask]
}

func (m *ShardedMap[K, V]) Set(key K, value V) {
	s := m.getShard(key)
	e := ttlEntry[V]{value: value}
	if m.expiry > 0 {
		e.expiry = time.Now().Add(m.expiry)
	}
	s.mu.Lock()
	s.data[key] = e
	s.mu.Unlock()
}

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
		delete(s.data, key)
		s.mu.Unlock()
		var zero V
		return zero, false
	}
	return e.value, true
}

func (m *ShardedMap[K, V]) Delete(key K) {
	m.getShard(key).mu.Lock()
	delete(m.getShard(key).data, key)
	m.getShard(key).mu.Unlock()
}

func (m *ShardedMap[K, V]) Has(key K) bool {
	_, ok := m.Get(key)
	return ok
}

func (m *ShardedMap[K, V]) Len() int {
	total := 0
	for _, s := range m.shards {
		s.mu.RLock()
		total += len(s.data)
		s.mu.RUnlock()
	}
	return total
}

func (m *ShardedMap[K, V]) Clear() {
	for _, s := range m.shards {
		s.mu.Lock()
		s.data = make(map[K]ttlEntry[V])
		s.mu.Unlock()
	}
}

func (m *ShardedMap[K, V]) Range(fn func(key K, value V) bool) {
	for _, s := range m.shards {
		s.mu.RLock()
		cont := true
		for k, e := range s.data {
			if e.expired() {
				continue
			}
			if !fn(k, e.value) {
				cont = false
				break
			}
		}
		s.mu.RUnlock()
		if !cont {
			break
		}
	}
}

func (m *ShardedMap[K, V]) GetOrSet(key K, defaultValue V) V {
	s := m.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.data[key]; ok && !e.expired() {
		return e.value
	}
	e := ttlEntry[V]{value: defaultValue}
	if m.expiry > 0 {
		e.expiry = time.Now().Add(m.expiry)
	}
	s.data[key] = e
	return defaultValue
}

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
	if m.expiry > 0 {
		ne.expiry = time.Now().Add(m.expiry)
	}
	s.data[key] = ne
	return newVal
}

func (m *ShardedMap[K, V]) Keys() []K {
	var all []K
	for _, s := range m.shards {
		s.mu.RLock()
		for k, e := range s.data {
			if !e.expired() {
				all = append(all, k)
			}
		}
		s.mu.RUnlock()
	}
	return all
}

func (m *ShardedMap[K, V]) Values() []V {
	var all []V
	for _, s := range m.shards {
		s.mu.RLock()
		for _, e := range s.data {
			if !e.expired() {
				all = append(all, e.value)
			}
		}
		s.mu.RUnlock()
	}
	return all
}

func StringHasher(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

