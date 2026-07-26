package lock

import (
	"context"
	"sync"
	"time"
)

// InMemoryLocker implements Locker using in-process mutexes.
type InMemoryLocker struct {
	mu    sync.Mutex
	locks map[string]*lockState
	next  uint64
}

type lockState struct {
	owner uint64
	timer *time.Timer
}

type memoryLease struct {
	locker *InMemoryLocker
	key    string
	owner  uint64
	once   sync.Once
}

func (l *memoryLease) Key() string {
	return l.key
}

func (l *memoryLease) Release(_ context.Context) error {
	l.once.Do(func() {
		l.locker.release(l.key, l.owner)
	})
	return nil
}

// NewInMemoryLocker creates a new in-process Locker.
func NewInMemoryLocker() *InMemoryLocker {
	return &InMemoryLocker{
		locks: make(map[string]*lockState),
	}
}

func (l *InMemoryLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (Lease, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		lease, ok, err := l.TryLock(ctx, key, ttl)
		if err != nil {
			return nil, err
		}
		if ok {
			return lease, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *InMemoryLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (Lease, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	l.mu.Lock()
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		return nil, false, nil
	}
	l.next++
	owner := l.next
	state := &lockState{owner: owner}
	l.locks[key] = state
	if ttl > 0 {
		state.timer = time.AfterFunc(ttl, func() {
			l.release(key, owner)
		})
	}
	l.mu.Unlock()
	return &memoryLease{locker: l, key: key, owner: owner}, true, nil
}

func (l *InMemoryLocker) release(key string, owner uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.locks[key]
	if !ok || state.owner != owner {
		return
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	delete(l.locks, key)
}
