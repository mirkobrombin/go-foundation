package pooling

import (
	"sync"
)

type Pool[T any] struct {
	pool      sync.Pool
	maxSize   int
	mu        sync.Mutex
	active    int
	finalizer func(T)
}

type Option[T any] func(*Pool[T])

func New[T any](factory func() T, opts ...Option[T]) *Pool[T] {
	p := &Pool[T]{}
	p.pool = sync.Pool{New: func() any { return factory() }}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func WithMaxSize[T any](n int) Option[T] {
	return func(p *Pool[T]) { p.maxSize = n }
}

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
