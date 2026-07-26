package broker

import "context"

// Handler processes a broker message.
type Handler func(ctx context.Context, payload []byte) error

// Subscription controls one broker subscription.
type Subscription interface {
	Unsubscribe() error
}

// Broker publishes messages and creates subscriptions.
type Broker interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(topic string, handler Handler) (Subscription, error)
}
