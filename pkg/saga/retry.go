package saga

import (
	"context"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/resiliency"
)

// RetryPolicy defines retry behavior for saga steps.
type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
	Multiplier  float64
}

// WithRetry wraps a function with retry logic according to the given policy.
func WithRetry(policy RetryPolicy, do func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		return resiliency.Retry(ctx, func() error {
			return do(ctx)
		},
			resiliency.WithAttempts(policy.MaxAttempts),
			resiliency.WithDelay(policy.Delay, 24*time.Hour),
			resiliency.WithFactor(policy.Multiplier),
		)
	}
}
