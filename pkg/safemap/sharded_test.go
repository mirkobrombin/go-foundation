package safemap

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"
)

func TestShardedMap_Basic(t *testing.T) {
	m := NewSharded[string, int](StringHasher, 4)

	m.Set("foo", 1)
	m.Set("bar", 2)

	if v, ok := m.Get("foo"); !ok || v != 1 {
		t.Errorf("Get(foo) = %v, %v; want 1, true", v, ok)
	}

	if v, ok := m.Get("bar"); !ok || v != 2 {
		t.Errorf("Get(bar) = %v, %v; want 2, true", v, ok)
	}

	if _, ok := m.Get("baz"); ok {
		t.Errorf("Get(baz) = _, true; want false")
	}

	if !m.Has("foo") {
		t.Errorf("Has(foo) = false; want true")
	}

	m.Delete("foo")
	if m.Has("foo") {
		t.Errorf("Has(foo) after delete = true; want false")
	}

	if m.Len() != 1 {
		t.Errorf("Len() = %d; want 1", m.Len())
	}

	m.Clear()
	if m.Len() != 0 {
		t.Errorf("Len() after Clear = %d; want 0", m.Len())
	}
}

func TestShardedMap_Range(t *testing.T) {
	m := NewSharded[string, int](StringHasher, 4)
	count := 100
	for i := 0; i < count; i++ {
		m.Set(fmt.Sprintf("key-%d", i), i)
	}

	seen := 0
	m.Range(func(k string, v int) bool {
		seen++
		return true
	})

	if seen != count {
		t.Errorf("Range visited %d items; want %d", seen, count)
	}
}

func TestShardedMap_Concurrent(t *testing.T) {
	m := NewSharded[string, int](StringHasher, 16)
	var wg sync.WaitGroup
	workers := 10
	ops := 1000

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				m.Set(key, j) // Write

				// Read same key or random key
				rKey := fmt.Sprintf("key-%d-%d", rand.Intn(workers), rand.Intn(ops))
				m.Get(rKey)

				if j%10 == 0 {
					m.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestShardedMap_GetOrSet(t *testing.T) {
	m := NewSharded[string, int](StringHasher, 4)
	v := m.GetOrSet("foo", 10)
	if v != 10 {
		t.Errorf("GetOrSet(foo) = %d; want 10", v)
	}

	v = m.GetOrSet("foo", 20)
	if v != 10 {
		t.Errorf("GetOrSet(foo) existing = %d; want 10", v)
	}
}

func TestShardedMap_Compute(t *testing.T) {
	m := NewSharded[string, int](StringHasher, 4)
	m.Set("foo", 1)

	v := m.Compute("foo", func(existing int, exists bool) int {
		return existing + 1
	})

	if v != 2 {
		t.Errorf("Compute(foo) = %d; want 2", v)
	}

	v = m.Compute("bar", func(existing int, exists bool) int {
		if !exists {
			return 5
		}
		return existing
	})

	if v != 5 {
		t.Errorf("Compute(bar) = %d; want 5", v)
	}
}

func TestShardedMap_KeysDistribution(t *testing.T) {
	// Verify that keys are actually distributed across shards
	// This uses internal knowledge check
	m := NewSharded[string, int](StringHasher, 4)
	count := 1000
	for i := 0; i < count; i++ {
		m.Set(strconv.Itoa(i), i)
	}

	// Each shard should have roughly count/4 items
	// Not strict check due to potential collision, but shouldn't be 0
	for i, shard := range m.shards {
		if len(shard.data) == 0 {
			t.Errorf("Shard %d is empty, bad distribution or small sample", i)
		}
	}
}
