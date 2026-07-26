package pooling

import (
	"sync"
)

// Pool is a generic object pool with optional max size and finalizer.
type Pool[T any] struct {
	pool      sync.Pool
	factory   func() T
	items     chan T
	maxSize   int
	finalizer func(T)
}

// Option configures a Pool.
type Option[T any] func(*Pool[T])

// New creates a new Pool using factory to create items.
func New[T any](factory func() T, opts ...Option[T]) *Pool[T] {
	p := &Pool[T]{factory: factory}
	p.pool = sync.Pool{New: func() any { return factory() }}
	for _, opt := range opts {
		opt(p)
	}
	if p.maxSize > 0 {
		p.items = make(chan T, p.maxSize)
	}
	return p
}

// WithMaxSize limits the number of retained items.
func WithMaxSize[T any](n int) Option[T] {
	return func(p *Pool[T]) { p.maxSize = n }
}

// WithFinalizer sets a cleanup function called before items are returned.
func WithFinalizer[T any](fn func(T)) Option[T] {
	return func(p *Pool[T]) { p.finalizer = fn }
}

func (p *Pool[T]) Get() T {
	if p.items != nil {
		select {
		case item := <-p.items:
			return item
		default:
			return p.factory()
		}
	}
	return p.pool.Get().(T)
}

func (p *Pool[T]) Put(item T) {
	if p.finalizer != nil {
		p.finalizer(item)
	}
	if p.items != nil {
		select {
		case p.items <- item:
		default:
		}
		return
	}
	p.pool.Put(item)
}
