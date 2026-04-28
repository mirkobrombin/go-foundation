package lock

import (
	"context"
	"sync"
	"time"
)

// InMemoryLocker implements Locker using in-process mutexes.
type InMemoryLocker struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

// NewInMemoryLocker creates a new in-process Locker.
func NewInMemoryLocker() *InMemoryLocker {
	return &InMemoryLocker{
		locks: make(map[string]struct{}),
	}
}

func (l *InMemoryLocker) Acquire(ctx context.Context, key string, ttl time.Duration) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		ok, err := l.TryLock(ctx, key, ttl)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *InMemoryLocker) TryLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.locks[key]; exists {
		return false, nil
	}
	l.locks[key] = struct{}{}
	return true, nil
}

func (l *InMemoryLocker) Release(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, key)
	return nil
}
