package broker

import (
	"context"
	"errors"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

var (
	_ = contracts.Assert[Broker]((*MemoryBroker)(nil))
	_ = contracts.Assert[Subscription]((*memorySubscription)(nil))
)

// MemoryBroker is an in-memory pub/sub implementation for testing and single-process use.
type MemoryBroker struct {
	subs  map[string]map[uint64]Handler
	subMu sync.RWMutex
	next  uint64
}

// NewMemoryBroker creates a MemoryBroker.
func NewMemoryBroker() *MemoryBroker {
	return &MemoryBroker{
		subs: make(map[string]map[uint64]Handler),
	}
}

// Publish sends a payload to all topic subscribers.
func (m *MemoryBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	m.subMu.RLock()
	topicHandlers := m.subs[topic]
	handlers := make([]Handler, 0, len(topicHandlers))
	for _, handler := range topicHandlers {
		handlers = append(handlers, handler)
	}
	m.subMu.RUnlock()

	var errs []error
	for _, h := range handlers {
		if err := func() (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = errors.New("relay: subscriber panic")
				}
			}()
			return h(ctx, append([]byte(nil), payload...))
		}(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Subscribe registers a handler for a topic.
func (m *MemoryBroker) Subscribe(topic string, handler Handler) (Subscription, error) {
	if topic == "" {
		return nil, errors.New("relay: subscription topic cannot be empty")
	}
	if handler == nil {
		return nil, errors.New("relay: subscription handler cannot be nil")
	}
	m.subMu.Lock()
	m.next++
	id := m.next
	if m.subs[topic] == nil {
		m.subs[topic] = make(map[uint64]Handler)
	}
	m.subs[topic][id] = handler
	m.subMu.Unlock()
	return &memorySubscription{broker: m, topic: topic, id: id}, nil
}

type memorySubscription struct {
	broker *MemoryBroker
	topic  string
	id     uint64
	once   sync.Once
}

func (s *memorySubscription) Unsubscribe() error {
	s.once.Do(func() {
		s.broker.subMu.Lock()
		delete(s.broker.subs[s.topic], s.id)
		if len(s.broker.subs[s.topic]) == 0 {
			delete(s.broker.subs, s.topic)
		}
		s.broker.subMu.Unlock()
	})
	return nil
}
