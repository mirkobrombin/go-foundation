package broker

import (
	"context"
	"sync"
)

// MemoryBroker is an in-memory message broker.
// MemoryBroker is an in-memory pub/sub implementation for testing and single-process use.
type MemoryBroker struct {
	subs  map[string][]func(ctx context.Context, payload []byte) error
	subMu sync.RWMutex
}

// NewMemoryBroker creates a MemoryBroker.
func NewMemoryBroker() *MemoryBroker {
	return &MemoryBroker{
		subs: make(map[string][]func(ctx context.Context, payload []byte) error),
	}
}

// Publish sends a payload to all topic subscribers.
func (m *MemoryBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	m.subMu.RLock()
	handlers := m.subs[topic]
	m.subMu.RUnlock()

	for _, h := range handlers {
		go func(fn func(ctx context.Context, payload []byte) error) {
			_ = fn(ctx, payload)
		}(h)
	}
	return nil
}

// Subscribe registers a handler for a topic.
func (m *MemoryBroker) Subscribe(topic string, handler func(ctx context.Context, payload []byte) error) error {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subs[topic] = append(m.subs[topic], handler)
	return nil
}