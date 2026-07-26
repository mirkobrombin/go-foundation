package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/core/options"
	"github.com/mirkobrombin/go-foundation/v2/core/safemap"
)

// Handler processes an event of type T.
type Handler[T any] func(ctx context.Context, event T) error

// Priority defines the ordering of event handlers.
type Priority int

const (
	// PriorityHigh runs the handler before normal and low priority.
	PriorityHigh Priority = 100
	// PriorityNormal is the default priority.
	PriorityNormal Priority = 0
	// PriorityLow runs the handler after all others.
	PriorityLow Priority = -100
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
	ctx   context.Context
	emit  func(ctx context.Context, event any) error
}

// OverflowStrategy controls behavior when the async channel is full.
type OverflowStrategy int

const (
	// OverflowFail returns an error immediately.
	OverflowFail OverflowStrategy = iota
	// OverflowDropOldest discards the oldest queued event.
	OverflowDropOldest
)

// Bus is the event bus that dispatches events to registered handlers.
type Bus struct {
	subscribers  *safemap.Map[reflect.Type, []subscriber]
	strategy     DispatchStrategy
	middlewares  []Middleware
	onAsyncError func(error)
	wildcard     []subscriber
	mu           sync.RWMutex

	asyncCh       chan asyncEvent
	asyncClose    chan struct{}
	asyncDone     chan struct{}
	asyncMu       sync.Mutex
	asyncWG       sync.WaitGroup
	asyncClosed   bool
	overflowStrat OverflowStrategy
	bufferSize    int
	closeOnce     sync.Once
	shutdownDone  chan struct{}
}

type subscriber struct {
	handler  any
	priority Priority
}

type asyncBusContextKey struct{}

// ErrReentrantShutdown is returned when an async handler tries to wait for itself.
var ErrReentrantShutdown = errors.New("events: async handler cannot wait for bus shutdown")

// ErrAsyncQueueFull is returned when an async event cannot be queued.
var ErrAsyncQueueFull = errors.New("events: async queue full")

// ErrBusClosed is returned when an async event is emitted after shutdown starts.
var ErrBusClosed = errors.New("events: bus is closed")

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
		asyncDone:     make(chan struct{}),
		shutdownDone:  make(chan struct{}),
		bufferSize:    1024,
		overflowStrat: OverflowFail,
	}
	options.Apply(b, opts...)
	go b.asyncProcessor()
	return b
}

// WithBufferSize sets the async channel buffer size.
func WithBufferSize(n int) Option {
	return func(b *Bus) {
		if n <= 0 {
			panic("events: buffer size must be positive")
		}
		b.bufferSize = n
		b.asyncCh = make(chan asyncEvent, n)
	}
}

// WithOverflowStrategy sets the overflow behavior for async events.
func WithOverflowStrategy(s OverflowStrategy) Option {
	return func(b *Bus) {
		if s != OverflowFail && s != OverflowDropOldest {
			panic("events: invalid overflow strategy")
		}
		b.overflowStrat = s
	}
}

func (b *Bus) asyncProcessor() {
	defer close(b.asyncDone)
	for {
		select {
		case <-b.asyncClose:
			for {
				select {
				case evt := <-b.asyncCh:
					b.processAsync(evt)
				default:
					return
				}
			}
		case evt := <-b.asyncCh:
			b.processAsync(evt)
		}
	}
}

func (b *Bus) processAsync(evt asyncEvent) {
	err := func() (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("events: async handler panic: %v", recovered)
			}
		}()
		ctx := context.WithValue(evt.ctx, asyncBusContextKey{}, b)
		return evt.emit(ctx, evt.event)
	}()
	if err != nil {
		b.reportAsyncError(err)
	}
}

func (b *Bus) reportAsyncError(err error) {
	b.mu.RLock()
	fn := b.onAsyncError
	b.mu.RUnlock()
	if fn == nil {
		return
	}
	func() {
		defer func() {
			_ = recover()
		}()
		fn(err)
	}()
}

// Close starts an asynchronous shutdown and never blocks the calling handler.
func (b *Bus) Close() {
	b.closeOnce.Do(func() {
		b.asyncMu.Lock()
		b.asyncClosed = true
		b.asyncMu.Unlock()
		go func() {
			b.asyncWG.Wait()
			close(b.asyncClose)
			<-b.asyncDone
			close(b.shutdownDone)
		}()
	})
}

// Shutdown drains queued events and waits for active handlers.
func (b *Bus) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if current, _ := ctx.Value(asyncBusContextKey{}).(*Bus); current == b {
		b.Close()
		return ErrReentrantShutdown
	}
	b.Close()
	select {
	case <-b.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
		newSubs := append([]subscriber(nil), subs...)
		newSubs = append(newSubs, subscriber{handler: fn, priority: p})
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
		if err := chain(ctx, event); err != nil {
			return err
		}
		return emitWildcards(ctx, b, event)
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

// EmitAny dispatches an event using its runtime type.
func EmitAny(ctx context.Context, b *Bus, event any) error {
	if b == nil {
		b = defaultBus
	}
	if event == nil {
		return nil
	}
	return emitByType(ctx, b, reflect.TypeOf(event), event)
}

func emitByType(ctx context.Context, b *Bus, key reflect.Type, event any) error {
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
			fn := reflect.ValueOf(sub.handler)
			results := fn.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(evt)})
			if len(results) == 1 && !results[0].IsNil() {
				err := results[0].Interface().(error)
				if b.strategy == StopOnFirstError {
					return err
				}
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}

	if len(mws) > 0 {
		chain := applyMiddleware(emit, mws)
		if err := chain(ctx, event); err != nil {
			return err
		}
		return emitWildcards(ctx, b, event)
	}

	if err := emit(ctx, event); err != nil {
		return err
	}

	return emitWildcards(ctx, b, event)
}

func emitWildcards(ctx context.Context, b *Bus, event any) error {
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
func EmitAsync[T any](ctx context.Context, b *Bus, event T) error {
	if b == nil {
		b = defaultBus
	}
	evt := asyncEvent{
		event: event,
		ctx:   asyncContext(ctx),
		emit: func(ctx context.Context, evt any) error {
			return Emit(ctx, b, evt.(T))
		},
	}

	return emitAsyncEvent(b, evt)
}

// EmitAnyAsync dispatches an event asynchronously using its runtime type.
func EmitAnyAsync(ctx context.Context, b *Bus, event any) error {
	if b == nil {
		b = defaultBus
	}
	if event == nil {
		return nil
	}
	return emitAsyncByType(ctx, b, reflect.TypeOf(event), event)
}

func emitAsyncByType(ctx context.Context, b *Bus, key reflect.Type, event any) error {
	evt := asyncEvent{
		event: event,
		ctx:   asyncContext(ctx),
		emit: func(ctx context.Context, evt any) error {
			return emitByType(ctx, b, key, evt)
		},
	}

	return emitAsyncEvent(b, evt)
}

func asyncContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func emitAsyncEvent(b *Bus, evt asyncEvent) error {
	b.asyncMu.Lock()
	if b.asyncClosed {
		b.asyncMu.Unlock()
		return ErrBusClosed
	}
	b.asyncWG.Add(1)
	b.asyncMu.Unlock()
	defer b.asyncWG.Done()

	switch b.overflowStrat {
	case OverflowDropOldest:
		select {
		case b.asyncCh <- evt:
			return nil
		default:
			select {
			case <-b.asyncCh:
			default:
			}
			select {
			case b.asyncCh <- evt:
				return nil
			default:
				return ErrAsyncQueueFull
			}
		}
	case OverflowFail:
		select {
		case b.asyncCh <- evt:
			return nil
		default:
			return ErrAsyncQueueFull
		}
	}
	return ErrAsyncQueueFull
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
