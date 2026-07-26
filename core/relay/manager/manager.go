package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/options"
	"github.com/mirkobrombin/go-foundation/v2/core/relay/broker"
)

// Job represents a unit of work with metadata.
type Job struct {
	ID        string
	Queue     string
	Topic     string
	Payload   []byte
	CreatedAt time.Time
	TryCount  int
}

// Handler processes a typed payload from the relay.
type Handler[T any] func(ctx context.Context, payload T) error

// Broker publishes and subscribes to topics.
type Broker = broker.Broker

const maxMessageSize = 4 << 20

// Relay coordinates typed message handlers with a pluggable broker.
type Relay struct {
	broker    Broker
	handlers  map[string]any
	handlerMu sync.RWMutex
	lifecycle sync.Mutex
	started   bool
	starting  bool
	stopping  bool
	stopStart bool
	runID     uint64
	cancel    context.CancelFunc
	subs      []broker.Subscription
}

// Option configures a Relay.
type Option = options.Option[Relay]

// New creates a Relay with the given options.
func New(opts ...Option) *Relay {
	r := &Relay{
		broker:   broker.NewMemoryBroker(),
		handlers: make(map[string]any),
	}
	options.Apply(r, opts...)
	if r.broker == nil {
		panic("relay: broker cannot be nil")
	}
	return r
}

// WithBroker sets the broker implementation.
func WithBroker(b Broker) Option {
	return func(r *Relay) {
		r.broker = b
	}
}

// Register adds a typed handler for a topic.
func Register[T any](r *Relay, topic string, fn Handler[T]) error {
	if r == nil {
		return errors.New("relay: relay cannot be nil")
	}
	if topic == "" {
		return errors.New("relay: topic cannot be empty")
	}
	if fn == nil {
		return errors.New("relay: handler cannot be nil")
	}
	wrapper := func(ctx context.Context, raw []byte) error {
		if len(raw) > maxMessageSize {
			return errors.New("relay: message exceeds 4 MiB limit")
		}
		var payload T
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("payload unmarshal failed: %w", err)
		}
		return fn(ctx, payload)
	}

	r.lifecycle.Lock()
	if r.started || r.starting || r.stopping {
		r.lifecycle.Unlock()
		return errors.New("relay: cannot register handlers after start")
	}
	r.handlerMu.Lock()
	if _, exists := r.handlers[topic]; exists {
		r.handlerMu.Unlock()
		r.lifecycle.Unlock()
		return fmt.Errorf("relay: topic %q already has a handler", topic)
	}
	r.handlers[topic] = wrapper
	r.handlerMu.Unlock()
	r.lifecycle.Unlock()
	return nil
}

// Enqueue publishes a typed payload to a topic.
func Enqueue[T any](ctx context.Context, r *Relay, topic string, payload T) error {
	if r == nil {
		return errors.New("relay: relay cannot be nil")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload marshal failed: %w", err)
	}
	if len(data) > maxMessageSize {
		return errors.New("relay: message exceeds 4 MiB limit")
	}

	return r.broker.Publish(ctx, topic, data)
}

// Start subscribes all registered handlers and returns a ready channel.
func (r *Relay) Start(ctx context.Context) (<-chan struct{}, error) {
	ready := make(chan struct{})
	if err := ctx.Err(); err != nil {
		close(ready)
		return ready, err
	}
	r.lifecycle.Lock()
	if r.started || r.starting || r.stopping {
		r.lifecycle.Unlock()
		close(ready)
		return ready, errors.New("relay: already started")
	}
	r.starting = true
	r.stopStart = false
	r.handlerMu.RLock()
	handlers := make(map[string]any, len(r.handlers))
	for topic, handler := range r.handlers {
		handlers[topic] = handler
	}
	r.handlerMu.RUnlock()
	r.lifecycle.Unlock()

	var subscriptions []broker.Subscription
	for topic, wrapperFn := range handlers {
		userHandler := wrapperFn.(func(ctx context.Context, data []byte) error)

		subscription, err := safeSubscribe(r.broker, topic, func(ctx context.Context, data []byte) (handlerErr error) {
			defer func() {
				if rec := recover(); rec != nil {
					handlerErr = fmt.Errorf("relay: panic in topic %q handler: %v", topic, rec)
				}
			}()

			return userHandler(ctx, data)
		})
		if err != nil {
			r.lifecycle.Lock()
			r.starting = false
			r.stopping = true
			r.lifecycle.Unlock()
			cleanupErr := unsubscribeAll(subscriptions)
			r.lifecycle.Lock()
			r.stopping = false
			r.lifecycle.Unlock()
			close(ready)
			return ready, errors.Join(err, cleanupErr)
		}
		if subscription == nil {
			r.lifecycle.Lock()
			r.starting = false
			r.stopping = true
			r.lifecycle.Unlock()
			cleanupErr := unsubscribeAll(subscriptions)
			r.lifecycle.Lock()
			r.stopping = false
			r.lifecycle.Unlock()
			close(ready)
			return ready, errors.Join(
				errors.New("relay: broker returned a nil subscription"),
				cleanupErr,
			)
		}
		subscriptions = append(subscriptions, subscription)
	}

	runCtx, cancel := context.WithCancel(ctx)
	r.lifecycle.Lock()
	if r.stopStart || ctx.Err() != nil {
		r.starting = false
		r.stopping = true
		r.lifecycle.Unlock()
		cancel()
		cleanupErr := unsubscribeAll(subscriptions)
		r.lifecycle.Lock()
		r.stopping = false
		r.lifecycle.Unlock()
		close(ready)
		return ready, errors.Join(errors.New("relay: start stopped"), ctx.Err(), cleanupErr)
	}
	r.runID++
	runID := r.runID
	r.cancel = cancel
	r.subs = subscriptions
	r.started = true
	r.starting = false
	r.stopStart = false
	r.lifecycle.Unlock()
	close(ready)
	go func() {
		<-runCtx.Done()
		_ = r.stopRun(runID)
	}()
	return ready, nil
}

// Stop removes active broker subscriptions.
func (r *Relay) Stop() error {
	return r.stopRun(0)
}

func (r *Relay) stopRun(runID uint64) error {
	r.lifecycle.Lock()
	if r.starting {
		r.stopStart = true
		r.lifecycle.Unlock()
		return nil
	}
	if !r.started || runID != 0 && runID != r.runID {
		r.lifecycle.Unlock()
		return nil
	}
	r.started = false
	r.stopping = true
	cancel := r.cancel
	r.cancel = nil
	subscriptions := r.subs
	r.subs = nil
	r.lifecycle.Unlock()
	if cancel != nil {
		cancel()
	}
	err := unsubscribeAll(subscriptions)
	r.lifecycle.Lock()
	r.stopping = false
	r.lifecycle.Unlock()
	return err
}

func safeSubscribe(
	b Broker,
	topic string,
	handler broker.Handler,
) (subscription broker.Subscription, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("relay: broker subscribe panic: %v", recovered)
		}
	}()
	return b.Subscribe(topic, handler)
}

func unsubscribeAll(subscriptions []broker.Subscription) error {
	var errs []error
	for index := len(subscriptions) - 1; index >= 0; index-- {
		subscription := subscriptions[index]
		err := func() (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("relay: unsubscribe panic: %v", recovered)
				}
			}()
			return subscription.Unsubscribe()
		}()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
