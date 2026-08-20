package plugin

import (
	"context"
	"sync"
)

// Registry manages plugin registration and lifecycle in a deterministic order.
type Registry struct {
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	order       []string
	byName      map[string]Plugin
	running     map[string]bool
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		byName:  make(map[string]Plugin),
		running: make(map[string]bool),
	}
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	plugin := r.byName[name]
	delete(r.byName, name)
	for i, current := range r.order {
		if current == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.mu.Unlock()
	if r.running[name] {
		delete(r.running, name)
		if plugin != nil {
			_ = plugin.Stop()
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	names := r.Names()
	var errs []error
	var started []string
	for _, name := range names {
		if r.running[name] {
			continue
		}
		p, ok := r.Get(name)
		if !ok {
			continue
		}
		if err := p.Start(); err != nil {
			errs = append(errs, err)
			for i := len(started) - 1; i >= 0; i-- {
				startedPlugin, exists := r.Get(started[i])
				if exists {
					if stopErr := startedPlugin.Stop(); stopErr != nil {
						errs = append(errs, stopErr)
						continue
					}
				}
				delete(r.running, started[i])
			}
			return errs
		}
		r.running[name] = true
		started = append(started, name)
	}
	return errs
}

// StopAll stops plugins in reverse insertion order and returns any collected errors.
func (r *Registry) StopAll() []error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	names := r.Names()
	var errs []error
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		if !r.running[name] {
			continue
		}
		p, ok := r.Get(name)
		if !ok {
			delete(r.running, name)
			continue
		}
		if err := p.Stop(); err != nil {
			errs = append(errs, err)
		} else {
			delete(r.running, name)
		}
	}
	return errs
}

// CloseAll stops plugins and closes context-aware resources in reverse insertion order.
func (r *Registry) CloseAll(ctx context.Context) []error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	names := r.Names()
	var errs []error
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		plugin, ok := r.Get(name)
		if !ok {
			continue
		}
		if r.running[name] {
			if err := plugin.Stop(); err != nil {
				errs = append(errs, err)
			}
			delete(r.running, name)
		}
		if closer, ok := plugin.(interface{ Close(context.Context) error }); ok {
			if err := closer.Close(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}
