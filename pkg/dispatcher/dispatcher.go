package dispatcher

import (
	"context"
	"fmt"
	"sync"
)

// HandlerFunc is a named dispatch handler.
type HandlerFunc func(ctx context.Context, payload ...any) (any, error)

// Dispatcher provides synchronous named dispatch of handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// New creates a new Dispatcher.
func New() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]HandlerFunc)}
}

// Register registers a handler by name. Panics if name is already registered.
func (d *Dispatcher) Register(name string, handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.handlers[name]; ok {
		panic(fmt.Sprintf("dispatcher: handler %q already registered", name))
	}
	d.handlers[name] = handler
}

// Dispatch calls the handler registered under name, passing the payload.
// Returns an error if no handler is found.
func (d *Dispatcher) Dispatch(ctx context.Context, name string, payload ...any) (any, error) {
	d.mu.RLock()
	handler, ok := d.handlers[name]
	d.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("dispatcher: no handler for %q", name)
	}
	return handler(ctx, payload...)
}

// Has returns true if a handler is registered under name.
func (d *Dispatcher) Has(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.handlers[name]
	return ok
}

// Names returns all registered handler names.
func (d *Dispatcher) Names() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	names := make([]string, 0, len(d.handlers))
	for name := range d.handlers {
		names = append(names, name)
	}
	return names
}