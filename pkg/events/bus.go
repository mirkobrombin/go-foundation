package events

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/options"
	"github.com/mirkobrombin/go-foundation/pkg/safemap"
)

type Handler[T any] func(ctx context.Context, event T) error

type Priority int

const (
	PriorityHigh   Priority = 100
	PriorityNormal Priority = 0
	PriorityLow    Priority = -100
)

type DispatchStrategy int

const (
	StopOnFirstError DispatchStrategy = iota
	BestEffort
)

type Middleware func(ctx context.Context, event any, next func(ctx context.Context, event any) error) error

type Bus struct {
	subscribers  *safemap.Map[reflect.Type, []subscriber]
	strategy     DispatchStrategy
	middlewares  []Middleware
	onAsyncError func(error)
	wildcard     []subscriber
	mu           sync.RWMutex
}

type subscriber struct {
	handler  any
	priority Priority
}

var defaultBus = New()

func Default() *Bus {
	return defaultBus
}

type Option = options.Option[Bus]

func New(opts ...Option) *Bus {
	b := &Bus{
		subscribers: safemap.New[reflect.Type, []subscriber](),
		strategy:    StopOnFirstError,
	}
	options.Apply(b, opts...)
	return b
}

func WithStrategy(s DispatchStrategy) Option {
	return func(b *Bus) { b.strategy = s }
}

func WithOnAsyncError(fn func(error)) Option {
	return func(b *Bus) { b.onAsyncError = fn }
}

func (b *Bus) Use(mw Middleware) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.middlewares = append(b.middlewares, mw)
}

func Subscribe[T any](b *Bus, fn Handler[T], priority ...Priority) {
	if b == nil {
		b = defaultBus
	}
	p := PriorityNormal
	if len(priority) > 0 {
		p = priority[0]
	}
	key := reflect.TypeFor[T]()
	b.subscribers.Compute(key, func(subs []subscriber, exists bool) []subscriber {
		newSubs := append(subs, subscriber{handler: fn, priority: p})
		sort.SliceStable(newSubs, func(i, j int) bool {
			return newSubs[i].priority > newSubs[j].priority
		})
		return newSubs
	})
}

func SubscribeWildcard(b *Bus, fn func(ctx context.Context, event any) error) {
	if b == nil {
		b = defaultBus
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wildcard = append(b.wildcard, subscriber{handler: fn})
}

func Emit[T any](ctx context.Context, b *Bus, event T) error {
	if b == nil {
		b = defaultBus
	}
	key := reflect.TypeFor[T]()
	subs, ok := b.subscribers.Get(key)

	b.mu.RLock()
	mws := b.middlewares
	b.mu.RUnlock()

	emit := func(ctx context.Context, evt any) error {
		if !ok {
			return nil
		}
		var errs []error
		for _, sub := range subs {
			if fn, ok := sub.handler.(Handler[T]); ok {
				if err := fn(ctx, evt.(T)); err != nil {
					if b.strategy == StopOnFirstError {
						return err
					}
					errs = append(errs, err)
				}
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}

	if len(mws) > 0 {
		chain := applyMiddleware(emit, mws)
		return chain(ctx, event)
	}

	if err := emit(ctx, event); err != nil {
		return err
	}

	b.mu.RLock()
	wildcards := b.wildcard
	b.mu.RUnlock()
	for _, w := range wildcards {
		if fn, ok := w.handler.(func(ctx context.Context, event any) error); ok {
			if err := fn(ctx, event); err != nil {
				return err
			}
		}
	}

	return nil
}

func EmitAsync[T any](ctx context.Context, b *Bus, event T) {
	if b == nil {
		b = defaultBus
	}
	go func() {
		if err := Emit(ctx, b, event); err != nil {
			b.mu.RLock()
			fn := b.onAsyncError
			b.mu.RUnlock()
			if fn != nil {
				fn(err)
			}
		}
	}()
}

func applyMiddleware(handler func(ctx context.Context, evt any) error, middlewares []Middleware) func(ctx context.Context, evt any) error {
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		next := handler
		handler = func(ctx context.Context, evt any) error {
			return mw(ctx, evt, next)
		}
	}
	return handler
}
