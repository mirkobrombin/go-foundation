package plugin

import (
	"sync"
)

// Registry manages plugin registration and lifecycle in a deterministic order.
type Registry struct {
	mu     sync.RWMutex
	order  []string
	byName map[string]Plugin
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Plugin)}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if _, ok := r.byName[name]; ok {
		return ErrAlreadyRegistered
	}

	r.byName[name] = p
	r.order = append(r.order, name)
	return nil
}

// Unregister removes a plugin by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.byName, name)
	for i, current := range r.order {
		if current == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Get returns a plugin and whether it exists.
func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.byName[name]
	return plugin, ok
}

// Names returns registered plugin names in insertion order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// StartAll starts plugins in insertion order and returns any collected errors.
func (r *Registry) StartAll() []error {
	names := r.Names()
	var errs []error
	for _, name := range names {
		p, ok := r.Get(name)
		if !ok {
			continue
		}
		if err := p.Start(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// StopAll stops plugins in reverse insertion order and returns any collected errors.
func (r *Registry) StopAll() []error {
	names := r.Names()
	var errs []error
	for i := len(names) - 1; i >= 0; i-- {
		p, ok := r.Get(names[i])
		if !ok {
			continue
		}
		if err := p.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
