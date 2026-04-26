package collections

import "sync"

// Set is a generic thread-safe set.
//
// Example:
//
//	s := collections.NewSet[string]()
//	s.Add("a", "b")
//	if s.Has("a") { ... }
type Set[T comparable] struct {
	items map[T]struct{}
	mu    sync.RWMutex
}

// NewSet creates a new empty Set.
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{
		items: make(map[T]struct{}),
	}
}

// Add adds elements to the set.
func (s *Set[T]) Add(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		s.items[item] = struct{}{}
	}
}

// Remove removes elements from the set.
func (s *Set[T]) Remove(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		delete(s.items, item)
	}
}

// Has checks if an element exists in the set.
func (s *Set[T]) Has(item T) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.items[item]
	return ok
}

// Len returns the number of elements in the set.
func (s *Set[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Items returns all elements in the set as a slice.
//
// Notes:
//
// The order of items is random.
func (s *Set[T]) Items() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]T, 0, len(s.items))
	for k := range s.items {
		res = append(res, k)
	}
	return res
}

// Clear removes all elements from the set.
func (s *Set[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[T]struct{})
}

// Union returns a new set with elements from both sets.
func (s *Set[T]) Union(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	s.mu.RLock()
	for k := range s.items {
		result.items[k] = struct{}{}
	}
	s.mu.RUnlock()
	other.mu.RLock()
	for k := range other.items {
		result.items[k] = struct{}{}
	}
	other.mu.RUnlock()
	return result
}

// Intersection returns a new set with elements common to both sets.
func (s *Set[T]) Intersection(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	s.mu.RLock()
	other.mu.RLock()
	defer s.mu.RUnlock()
	defer other.mu.RUnlock()
	for k := range s.items {
		if _, ok := other.items[k]; ok {
			result.items[k] = struct{}{}
		}
	}
	return result
}

// Difference returns a new set with elements in s but not in other.
func (s *Set[T]) Difference(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	s.mu.RLock()
	other.mu.RLock()
	defer s.mu.RUnlock()
	defer other.mu.RUnlock()
	for k := range s.items {
		if _, ok := other.items[k]; !ok {
			result.items[k] = struct{}{}
		}
	}
	return result
}

// ForEach calls fn for each element. Iteration stops if fn returns false.
func (s *Set[T]) ForEach(fn func(T) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k := range s.items {
		if !fn(k) {
			return
		}
	}
}
