package pipeline

import (
	"context"
)

// Middleware wraps a pipeline handler with cross-cutting behavior.
type Middleware[T, U any] func(ctx context.Context, input T, next func(context.Context, T) (U, error)) (U, error)

// Pipeline chains Middleware functions around a final handler.
type Pipeline[T, U any] struct {
	middlewares []Middleware[T, U]
	handler     func(context.Context, T) (U, error)
}

// New creates an empty Pipeline.
func New[T, U any]() *Pipeline[T, U] {
	return &Pipeline[T, U]{}
}

func (p *Pipeline[T, U]) Use(mw Middleware[T, U]) *Pipeline[T, U] {
	p.middlewares = append(p.middlewares, mw)
	return p
}

func (p *Pipeline[T, U]) Then(fn func(context.Context, T) (U, error)) *Pipeline[T, U] {
	p.handler = fn
	return p
}

func (p *Pipeline[T, U]) Process(ctx context.Context, input T) (U, error) {
	if p.handler == nil {
		var zero U
		return zero, nil
	}

	handler := p.handler
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		mw := p.middlewares[i]
		next := handler
		handler = func(ctx context.Context, input T) (U, error) {
			return mw(ctx, input, next)
		}
	}
	return handler(ctx, input)
}
