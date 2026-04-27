package resiliency

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// ErrBulkheadFull is returned by Bulkhead when the queue is full.
var ErrBulkheadFull = errors.New("bulkhead: queue is full")

// RateLimiter limits the rate of operations using a token bucket algorithm.
//
// Example:
//
//	rl := resiliency.NewRateLimiter(100, 50)
//	if err := rl.Wait(ctx); err != nil { ... }
type RateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64
	burst    float64
	lastTime time.Time
}

// NewRateLimiter creates a token bucket rate limiter.
func NewRateLimiter(rate, burst int) *RateLimiter {
	return &RateLimiter{
		tokens:   float64(burst),
		rate:     float64(rate),
		burst:    float64(burst),
		lastTime: time.Now(),
	}
}

// Allow reports whether an operation is allowed now without blocking.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.tokens += rl.rate * now.Sub(rl.lastTime).Seconds()
	rl.lastTime = now
	if rl.tokens > rl.burst {
		rl.tokens = rl.burst
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		if rl.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(math.Ceil(1.0/rl.rate*1e9)) * time.Nanosecond):
		}
	}
}

// Bulkhead limits the number of concurrent operations with a queue.
//
// Example:
//
//	bh := resiliency.NewBulkhead(10, 5)
//	if err := bh.Execute(ctx, func() error { ... }); err != nil { ... }
type Bulkhead struct {
	sem chan struct{}
}

// NewBulkhead creates a bulkhead with maxConcurrent slots and maxQueue waiters.
func NewBulkhead(maxConcurrent, maxQueue int) *Bulkhead {
	return &Bulkhead{
		sem: make(chan struct{}, maxConcurrent+maxQueue),
	}
}

// Execute runs fn if a slot is available, or returns ErrBulkheadFull if the queue is full.
func (b *Bulkhead) Execute(ctx context.Context, fn func() error) error {
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	}
}
