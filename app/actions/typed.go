package actions

import (
	"context"
	"fmt"
)

// Typed identifies an action with compile-time payload and result types.
type Typed[P, R any] struct {
	name string
}

// NewTyped creates a typed action.
func NewTyped[P, R any](name string) Typed[P, R] {
	if name == "" {
		panic("actions: action name cannot be empty")
	}
	return Typed[P, R]{name: name}
}

// Name returns the action name.
func (a Typed[P, R]) Name() string {
	return a.name
}

// HandleTyped registers a typed action handler.
func HandleTyped[P, R any](r *Router, action Typed[P, R], handler func(context.Context, P) (R, error)) error {
	if handler == nil {
		return fmt.Errorf("actions: typed handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[action.name]; exists {
		return fmt.Errorf("actions: handler %q already registered", action.name)
	}
	if _, exists := r.typed[action.name]; exists {
		return fmt.Errorf("actions: handler %q already registered", action.name)
	}
	r.typed[action.name] = func(ctx context.Context, payload any) (any, error) {
		typed, ok := payload.(P)
		if !ok {
			return nil, fmt.Errorf("actions: payload for %q has type %T", action.name, payload)
		}
		return handler(ctx, typed)
	}
	return nil
}

// DispatchTyped dispatches an action with compile-time payload and result types.
func DispatchTyped[P, R any](ctx context.Context, r *Router, action Typed[P, R], payload P) (R, error) {
	var zero R

	r.mu.RLock()
	handler := r.typed[action.name]
	r.mu.RUnlock()
	if handler == nil {
		return zero, fmt.Errorf("actions: no handler for %q", action.name)
	}
	result, err := handler(ctx, payload)
	if err != nil {
		return zero, err
	}
	typed, ok := result.(R)
	if !ok {
		return zero, fmt.Errorf("actions: result for %q has type %T", action.name, result)
	}
	return typed, nil
}
