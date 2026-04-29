package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/mirkobrombin/go-foundation/pkg/options"
	"github.com/mirkobrombin/go-foundation/pkg/safemap"
)

// Handler processes an event of type T.
type Handler[T any] func(ctx context.Context, event T) error

// Priority defines the ordering of event handlers.
type Priority int

const (
	// PriorityHigh runs the handler before normal and low priority.
	PriorityHigh   Priority = 100
	// PriorityNormal is the default priority.
	PriorityNormal Priority = 0
	// PriorityLow runs the handler after all others.
	PriorityLow    Priority = -100
)

// DispatchStrategy controls error handling during event dispatch.
type DispatchStrategy int

const (
	// StopOnFirstError stops dispatch on the first handler error.
	StopOnFirstError DispatchStrategy = iota
	// BestEffort continues dispatch even if handlers fail.
	BestEffort
)

// Middleware wraps event dispatch with cross-cutting behavior.
type Middleware func(ctx context.Context, event any, next func(ctx context.Context, event any) error) error

type asyncEvent struct {
	event any
	emit  func(ctx context.Context, event any) error
}

// OverflowStrategy controls behavior when the async channel is full.
type OverflowStrategy int

const (
	// OverflowBlock blocks the emitter until space is available.
	OverflowBlock OverflowStrategy = iota
	// OverflowDropOldest discards the oldest queued event.
	OverflowDropOldest
	// OverflowFail returns an error immediately.
	OverflowFail
)

// Bus is the event bus that dispatches events to registered handlers.
type Bus struct {
	subscribers    *safemap.Map[reflect.Type, []subscriber]
	strategy       DispatchStrategy
	middlewares    []Middleware
	onAsyncError   func(error)
	wildcard       []subscriber
	mu             sync.RWMutex

	asyncCh        chan asyncEvent
	asyncClose     chan struct{}
	overflowStrat  OverflowStrategy
	bufferSize     int
}

type subscriber struct {
	handler  any
	priority Priority
}

var defaultBus = New()

// Default returns the package-level default Bus.
func Default() *Bus {
	return defaultBus
}

// Option configures a Bus.
type Option = options.Option[Bus]

// New creates a new Bus with the given options.
func New(opts ...Option) *Bus {
	b := &Bus{
		subscribers:   safemap.New[reflect.Type, []subscriber](),
		strategy:      StopOnFirstError,
		asyncCh:       make(chan asyncEvent, 1024),
		asyncClose:    make(chan struct{}),
		bufferSize:    1024,
		overflowStrat: OverflowBlock,
	}
	go b.asyncProcessor()
	options.Apply(b, opts...)
	return b
}

// WithBufferSize sets the async channel buffer size.
func WithBufferSize(n int) Option {
	return func(b *Bus) {
		b.bufferSize = n
		b.asyncCh = make(chan asyncEvent, n)
	}
}

// WithOverflowStrategy sets the overflow behavior for async events.
func WithOverflowStrategy(s OverflowStrategy) Option {
	return func(b *Bus) { b.overflowStrat = s }
}

func (b *Bus) asyncProcessor() {
	for {
		select {
		case <-b.asyncClose:
			return
		case evt, ok := <-b.asyncCh:
			if !ok {
				return
			}
			if err := evt.emit(context.Background(), evt.event); err != nil {
				b.mu.RLock()
				fn := b.onAsyncError
				b.mu.RUnlock()
				if fn != nil {
					fn(err)
				}
			}
		}
	}
}

// Close shuts down the async event processor.
func (b *Bus) Close() {
	close(b.asyncClose)
}

// WithStrategy sets the dispatch strategy for the Bus.
func WithStrategy(s DispatchStrategy) Option {
	return func(b *Bus) { b.strategy = s }
}

// WithOnAsyncError sets the error handler for async event emissions.
func WithOnAsyncError(fn func(error)) Option {
	return func(b *Bus) { b.onAsyncError = fn }
}

// Use appends middleware to the bus.
func (b *Bus) Use(mw Middleware) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.middlewares = append(b.middlewares, mw)
}

// Subscribe registers a typed handler for events of type T.
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

// SubscribeWildcard registers a handler for all event types.
func SubscribeWildcard(b *Bus, fn func(ctx context.Context, event any) error) {
	if b == nil {
		b = defaultBus
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wildcard = append(b.wildcard, subscriber{handler: fn})
}

// Emit dispatches an event to all matching handlers synchronously.
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

// EmitAsync dispatches an event asynchronously to the bus channel.
func EmitAsync[T any](ctx context.Context, b *Bus, event T) {
	if b == nil {
		b = defaultBus
	}
	evt := asyncEvent{
		event: event,
		emit: func(ctx context.Context, evt any) error {
			key := reflect.TypeFor[T]()
			subs, ok := b.subscribers.Get(key)
			if !ok {
				return nil
			}
			for _, sub := range subs {
				if fn, ok := sub.handler.(Handler[T]); ok {
					if err := fn(ctx, evt.(T)); err != nil {
						if b.strategy == StopOnFirstError {
							return err
						}
					}
				}
			}
			return nil
		},
	}

	switch b.overflowStrat {
	case OverflowBlock:
		b.asyncCh <- evt
	case OverflowDropOldest:
		select {
		case b.asyncCh <- evt:
		default:
			select {
			case <-b.asyncCh:
			default:
			}
			b.asyncCh <- evt
		}
	case OverflowFail:
		select {
		case b.asyncCh <- evt:
		default:
			if b.onAsyncError != nil {
				b.onAsyncError(fmt.Errorf("events: async channel full, event dropped"))
			}
		}
	}
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
