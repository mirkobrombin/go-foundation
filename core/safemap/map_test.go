package safemap

import (
	"sync"
	"testing"
)

func TestMap_Basic(t *testing.T) {
	m := New[string, int]()

	m.Set("a", 1)
	m.Set("b", 2)

	if v, ok := m.Get("a"); !ok || v != 1 {
		t.Errorf("Get a: got %d, want 1", v)
	}

	if !m.Has("b") {
		t.Error("Has b: should be true")
	}

	if m.Has("c") {
		t.Error("Has c: should be false")
	}

	if m.Len() != 2 {
		t.Errorf("Len: got %d, want 2", m.Len())
	}
}

func TestMap_Delete(t *testing.T) {
	m := New[string, string]()
	m.Set("key", "value")
	m.Delete("key")

	if m.Has("key") {
		t.Error("key should be deleted")
	}
}

func TestMap_Clear(t *testing.T) {
	m := New[int, int]()
	m.Set(1, 100)
	m.Set(2, 200)
	m.Clear()

	if m.Len() != 0 {
		t.Errorf("Clear: Len should be 0, got %d", m.Len())
	}
}

func TestMap_Keys(t *testing.T) {
	m := New[string, int]()
	m.Set("x", 1)
	m.Set("y", 2)

	keys := m.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys: got %d, want 2", len(keys))
	}
}

func TestMap_Values(t *testing.T) {
	m := New[string, int]()
	m.Set("x", 10)
	m.Set("y", 20)

	vals := m.Values()
	if len(vals) != 2 {
		t.Errorf("Values: got %d, want 2", len(vals))
	}
}

func TestMap_Range(t *testing.T) {
	m := New[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return count < 2
	})

	if count != 2 {
		t.Errorf("Range should stop early, got %d iterations", count)
	}
}

func TestMap_GetOrSet(t *testing.T) {
	m := New[string, int]()

	v1 := m.GetOrSet("key", 42)
	if v1 != 42 {
		t.Errorf("GetOrSet first call: got %d, want 42", v1)
	}

	v2 := m.GetOrSet("key", 100)
	if v2 != 42 {
		t.Errorf("GetOrSet second call: got %d, want 42", v2)
	}
}

func TestMap_Compute(t *testing.T) {
	m := New[string, int]()

	m.Compute("counter", func(v int, exists bool) int {
		return v + 1
	})

	if v, _ := m.Get("counter"); v != 1 {
		t.Errorf("Compute first: got %d, want 1", v)
	}

	m.Compute("counter", func(v int, exists bool) int {
		return v + 1
	})

	if v, _ := m.Get("counter"); v != 2 {
		t.Errorf("Compute second: got %d, want 2", v)
	}
}

func TestMap_Concurrent(t *testing.T) {
	m := New[int, int]()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Set(n, n*10)
			m.Get(n)
			m.Has(n)
		}(i)
	}

	wg.Wait()

	if m.Len() != 100 {
		t.Errorf("Concurrent: got %d items, want 100", m.Len())
	}
}
