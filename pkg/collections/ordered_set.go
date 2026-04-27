package collections

import (
	"sync"
)

// OrderedSet is a thread-safe set that preserves insertion order.
//
// Example:
//
//	s := collections.NewOrderedSet[string]()
//	s.Add("a", "b")
//	for _, v := range s.Items() { ... }
type OrderedSet[T comparable] struct {
	mu    sync.RWMutex
	items map[T]struct{}
	order []T
}

// NewOrderedSet creates a new OrderedSet.
func NewOrderedSet[T comparable]() *OrderedSet[T] {
	return &OrderedSet[T]{
		items: make(map[T]struct{}),
	}
}

// Add adds elements to the set, preserving insertion order.
func (s *OrderedSet[T]) Add(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		if _, ok := s.items[item]; !ok {
			s.items[item] = struct{}{}
			s.order = append(s.order, item)
		}
	}
}

// Remove removes elements from the set.
func (s *OrderedSet[T]) Remove(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		if _, ok := s.items[item]; ok {
			delete(s.items, item)
			for i, v := range s.order {
				if v == item {
					s.order = append(s.order[:i], s.order[i+1:]...)
					break
				}
			}
		}
	}
}

// Has checks if an element exists.
func (s *OrderedSet[T]) Has(item T) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.items[item]
	return ok
}

// Len returns the number of elements.
func (s *OrderedSet[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Items returns all elements in insertion order.
func (s *OrderedSet[T]) Items() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]T, len(s.order))
	copy(res, s.order)
	return res
}
