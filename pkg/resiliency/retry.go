package resiliency

import (
	"context"
	"math"
	"time"
)

// RetryOptions configures retry behavior.
type RetryOptions struct {
	Attempts     int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
}

// DefaultRetryOptions provides reasonable defaults.
var DefaultRetryOptions = RetryOptions{
	Attempts:     3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     2 * time.Second,
	Factor:       2.0,
}

// Retry executes fn up to Attempts times with exponential backoff.
//
// Example:
//
//	err := resiliency.Retry(ctx, func() error {
//		return doNetworkCall()
//	}, resiliency.WithAttempts(5))
func Retry(ctx context.Context, fn func() error, opts ...func(*RetryOptions)) error {
	o := DefaultRetryOptions
	for _, opt := range opts {
		opt(&o)
	}

	var lastErr error
	for i := 0; i < o.Attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if i < o.Attempts-1 {
			delay := time.Duration(float64(o.InitialDelay) * math.Pow(o.Factor, float64(i)))
			if delay > o.MaxDelay {
				delay = o.MaxDelay
			}

			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	return lastErr
}

// WithAttempts sets the maximum number of retry attempts.
func WithAttempts(n int) func(*RetryOptions) {
	return func(o *RetryOptions) { o.Attempts = n }
}

// WithDelay sets the initial and max delay for backoff.
func WithDelay(initial, max time.Duration) func(*RetryOptions) {
	return func(o *RetryOptions) {
		o.InitialDelay = initial
		o.MaxDelay = max
	}
}

// WithFactor sets the backoff factor.
func WithFactor(f float64) func(*RetryOptions) {
	return func(o *RetryOptions) { o.Factor = f }
}
