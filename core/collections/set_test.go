package collections

import (
	"testing"
	"time"
)

func TestSet(t *testing.T) {
	s := NewSet[int]()

	s.Add(1, 2, 3)
	if s.Len() != 3 {
		t.Errorf("Len: got %d, want 3", s.Len())
	}

	if !s.Has(2) {
		t.Error("Has 2: should be true")
	}

	s.Remove(2)
	if s.Has(2) {
		t.Error("Has 2: should be false after removal")
	}

	items := s.Items()
	if len(items) != 2 {
		t.Errorf("Items: got %d, want 2", len(items))
	}

	s.Clear()
	if s.Len() != 0 {
		t.Error("Clear should empty the set")
	}
}

func TestSetForEachAllowsMutation(t *testing.T) {
	set := NewSet[int]()
	set.Add(1, 2, 3)
	done := make(chan struct{})
	go func() {
		set.ForEach(func(value int) bool {
			set.Remove(value)
			return true
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ForEach() deadlocked when the callback mutated the set")
	}
	if set.Len() != 0 {
		t.Fatalf("set length = %d, want 0", set.Len())
	}
}

func TestMultiMapGetReturnsCopy(t *testing.T) {
	mapping := NewMultiMap[string, int]()
	mapping.Add("key", 1)
	values := mapping.Get("key")
	values[0] = 2
	if got := mapping.Get("key")[0]; got != 1 {
		t.Fatalf("Get() exposed internal slice: %d", got)
	}
}
