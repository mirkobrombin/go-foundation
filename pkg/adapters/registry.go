package adapters

import (
	"sync"
)

// Registry provides a generic, thread-safe registry for pluggable adapters.
//
// Example:
//
//	r := adapters.NewRegistry[Store]()
//	r.Register("memory", NewMemoryStore())
//	r.SetDefault("memory")
type Registry[T any] struct {
	adapters    map[string]T
	defaultName string
	mu          sync.RWMutex
	onRegister  []func(name string, adapter T)
	onRemove    []func(name string)
}

// NewRegistry creates an empty adapter registry.
func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{
		adapters: make(map[string]T),
	}
}

// Register adds an adapter with the given name.
func (r *Registry[T]) Register(name string, adapter T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = adapter
	for _, fn := range r.onRegister {
		fn(name, adapter)
	}
}

// Get retrieves an adapter by name.
//
// Returns:
//
// The adapter and true if found, otherwise zero value and false.
func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.adapters[name]
	return v, ok
}

// MustGet retrieves an adapter and panics if not found.
func (r *Registry[T]) MustGet(name string) T {
	v, ok := r.Get(name)
	if !ok {
		panic("adapters: not found: " + name)
	}
	return v
}

// SetDefault sets the default adapter name.
func (r *Registry[T]) SetDefault(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultName = name
}

// Default returns the default adapter.
//
// Notes:
//
// Panics if no default is set or default is not registered.
func (r *Registry[T]) Default() T {
	r.mu.RLock()
	name := r.defaultName
	r.mu.RUnlock()

	if name == "" {
		panic("adapters: no default set")
	}
	return r.MustGet(name)
}

// DefaultOr returns the default adapter, or the provided fallback if no default is set.
func (r *Registry[T]) DefaultOr(fallback T) T {
	r.mu.RLock()
	name := r.defaultName
	r.mu.RUnlock()

	if name == "" {
		return fallback
	}

	v, ok := r.Get(name)
	if !ok {
		return fallback
	}
	return v
}

// Has checks if an adapter is registered.
func (r *Registry[T]) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.adapters[name]
	return ok
}

// Names returns all registered adapter names.
func (r *Registry[T]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		names = append(names, k)
	}
	return names
}

// Remove unregisters an adapter.
func (r *Registry[T]) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, name)
	if r.defaultName == name {
		r.defaultName = ""
	}
	for _, fn := range r.onRemove {
		fn(name)
	}
}

// OnRegister registers a callback that fires when an adapter is registered.
func (r *Registry[T]) OnRegister(fn func(name string, adapter T)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRegister = append(r.onRegister, fn)
}

// OnRemove registers a callback that fires when an adapter is removed.
func (r *Registry[T]) OnRemove(fn func(name string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRemove = append(r.onRemove, fn)
}

// Clear removes all adapters.
func (r *Registry[T]) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = make(map[string]T)
	r.defaultName = ""
	r.onRegister = nil
	r.onRemove = nil
}
