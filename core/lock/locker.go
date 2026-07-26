package lock

import (
	"context"
	"time"
)

// Locker defines the interface for distributed or local locks.
//
// Example:
//
//	// Assuming a RedisLocker implementation
//	locker := NewRedisLocker(redisClient)
//	lease, err := locker.Acquire(ctx, "my-resource", 5*time.Second)
//	if err == nil {
//		defer lease.Release(ctx)
//	}
type Lease interface {
	Key() string
	Release(ctx context.Context) error
}

type Locker interface {
	// Acquire blocks until the lock for key is obtained or the context is cancelled.
	// If ttl is greater than zero, the lock is automatically released after the duration.
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lease, error)

	// TryLock attempts to obtain the lock without waiting. It returns true on success.
	TryLock(ctx context.Context, key string, ttl time.Duration) (Lease, bool, error)
}
