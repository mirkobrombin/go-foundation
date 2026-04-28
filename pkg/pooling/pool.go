package pooling

import (
	"sync"
)

// Pool is a generic object pool with optional max size and finalizer.
type Pool[T any] struct {
	pool      sync.Pool
	maxSize   int
	mu        sync.Mutex
	active    int
	finalizer func(T)
}

// Option configures a Pool.
type Option[T any] func(*Pool[T])

// New creates a new Pool using factory to create items.
func New[T any](factory func() T, opts ...Option[T]) *Pool[T] {
	p := &Pool[T]{}
	p.pool = sync.Pool{New: func() any { return factory() }}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithMaxSize limits the pool to n active items.
func WithMaxSize[T any](n int) Option[T] {
	return func(p *Pool[T]) { p.maxSize = n }
}

// WithFinalizer sets a cleanup function called before items are returned.
func WithFinalizer[T any](fn func(T)) Option[T] {
	return func(p *Pool[T]) { p.finalizer = fn }
}

func (p *Pool[T]) Get() T {
	return p.pool.Get().(T)
}

func (p *Pool[T]) Put(item T) {
	if p.finalizer != nil {
		p.finalizer(item)
	}
	if p.maxSize > 0 {
		p.mu.Lock()
		if p.active >= p.maxSize {
			p.mu.Unlock()
			return
		}
		p.active++
		p.mu.Unlock()
	}
	p.pool.Put(item)
}
