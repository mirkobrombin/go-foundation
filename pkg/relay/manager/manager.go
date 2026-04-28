package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/options"
	"github.com/mirkobrombin/go-foundation/pkg/relay/broker"
)

// Job represents a relay job with topic and payload.
// Job represents a unit of work with metadata.
type Job struct {
	ID        string
	Queue     string
	Topic     string
	Payload   []byte
	CreatedAt time.Time
	TryCount  int
}

// Handler processes a typed relay payload.
// Handler processes a typed payload from the relay.
type Handler[T any] func(ctx context.Context, payload T) error

// Broker publishes and subscribes to topics.
type Broker interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(topic string, handler func(ctx context.Context, payload []byte) error) error
}

// Relay manages broker subscriptions and handlers.
// Relay coordinates typed message handlers with a pluggable broker.
type Relay struct {
	broker    Broker
	handlers  map[string]any
	handlerMu sync.RWMutex
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
	return r
}

// WithBroker sets the broker implementation.
func WithBroker(b Broker) Option {
	return func(r *Relay) {
		r.broker = b
	}
}

// Register adds a typed handler for a topic.
func Register[T any](r *Relay, topic string, fn Handler[T]) {
	wrapper := func(ctx context.Context, raw []byte) error {
		var payload T
		if err := json.Unmarshal(raw, &payload); err != nil {
			return fmt.Errorf("payload unmarshal failed: %w", err)
		}
		return fn(ctx, payload)
	}

	r.handlerMu.Lock()
	r.handlers[topic] = wrapper
	r.handlerMu.Unlock()
}

// Enqueue publishes a typed payload to a topic.
func Enqueue[T any](ctx context.Context, r *Relay, topic string, payload T) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload marshal failed: %w", err)
	}

	return r.broker.Publish(ctx, topic, data)
}

// Start subscribes all registered handlers and returns a ready channel.
func (r *Relay) Start(ctx context.Context) (<-chan struct{}, error) {
	r.handlerMu.RLock()
	defer r.handlerMu.RUnlock()

	ready := make(chan struct{})

	for topic, wrapperFn := range r.handlers {
		userHandler := wrapperFn.(func(ctx context.Context, data []byte) error)

		err := r.broker.Subscribe(topic, func(ctx context.Context, data []byte) error {
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Printf("panic in job %s: %v\n", topic, rec)
				}
			}()

			return userHandler(ctx, data)
		})
		if err != nil {
			close(ready)
			return ready, err
		}
	}

	close(ready)
	return ready, nil
}