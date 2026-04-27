package hooks

import (
	"context"
	"time"
)

// HookFunc is a function called at a lifecycle event.
type HookFunc func(ctx context.Context, key string, args []any) error

// Runner manages execution of lifecycle hooks with before/after patterns.
type Runner struct {
	discovery *Discovery
	before    map[string][]HookFunc
	after     map[string][]HookFunc
}

// NewRunner creates a hook runner with a shared discovery instance.
func NewRunner() *Runner {
	return &Runner{
		discovery: NewDiscovery(),
		before:    make(map[string][]HookFunc),
		after:     make(map[string][]HookFunc),
	}
}

// Before registers a function to be called before a specific event.
func (r *Runner) Before(key string, fn HookFunc) {
	r.before[key] = append(r.before[key], fn)
}

// After registers a function to be called after a specific event.
func (r *Runner) After(key string, fn HookFunc) {
	r.after[key] = append(r.after[key], fn)
}

// BeforeAll registers a function to be called before any event.
func (r *Runner) BeforeAll(fn HookFunc) {
	r.before["*"] = append(r.before["*"], fn)
}

// AfterAll registers a function to be called after any event.
func (r *Runner) AfterAll(fn HookFunc) {
	r.after["*"] = append(r.after["*"], fn)
}

// Run executes before hooks, the action, and after hooks.
func (r *Runner) Run(ctx context.Context, key string, action func() error, args ...any) error {
	if err := r.runHooks(ctx, key, r.before, args); err != nil {
		return err
	}

	if err := action(); err != nil {
		return err
	}

	return r.runHooks(ctx, key, r.after, args)
}

// RunParallel executes hooks concurrently. Each hook runs in its own goroutine.
// If any hook returns an error, cancellation is propagated via context.
//
// Example:
//
//	err := r.RunParallel(ctx, "my-event", args)
func (r *Runner) RunParallel(ctx context.Context, key string, args ...any) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	all := append(r.before["*"], r.before[key]...)
	all = append(all, r.after["*"]...)
	all = append(all, r.after[key]...)

	errCh := make(chan error, len(all))
	for _, fn := range all {
		fn := fn
		go func() {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
			default:
				errCh <- fn(ctx, key, args)
			}
		}()
	}

	var firstErr error
	for i := 0; i < len(all); i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	return firstErr
}

func (r *Runner) runHooks(ctx context.Context, key string, hooks map[string][]HookFunc, args []any) error {
	// Global hooks first
	for _, fn := range hooks["*"] {
		if err := fn(ctx, key, args); err != nil {
			return err
		}
	}

	// Specific hooks
	for _, fn := range hooks[key] {
		if err := fn(ctx, key, args); err != nil {
			return err
		}
	}

	return nil
}

// Discovery returns the underlying discovery instance.
func (r *Runner) Discovery() *Discovery {
	return r.discovery
}

// Clear removes all registered hooks.
func (r *Runner) Clear() {
	r.before = make(map[string][]HookFunc)
	r.after = make(map[string][]HookFunc)
}

// RunWithTimeout runs an action with a context deadline.
//
// Example:
//
//	err := hooks.RunWithTimeout(ctx, time.Second, func() error { ... })
func RunWithTimeout(ctx context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(ctx)
}
