package resiliency

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	t.Run("SuccessFirstTry", func(t *testing.T) {
		calls := 0
		err := Retry(context.Background(), func() error {
			calls++
			return nil
		})
		if err != nil || calls != 1 {
			t.Errorf("Retry failed: %v, calls=%d", err, calls)
		}
	})

	t.Run("SuccessAfterRetries", func(t *testing.T) {
		calls := 0
		err := Retry(context.Background(), func() error {
			calls++
			if calls < 3 {
				return errors.New("fail")
			}
			return nil
		}, WithAttempts(5), WithDelay(1*time.Millisecond, 10*time.Millisecond))

		if err != nil || calls != 3 {
			t.Errorf("Retry failed: %v, calls=%d", err, calls)
		}
	})

	t.Run("FailureAllAttempts", func(t *testing.T) {
		calls := 0
		targetErr := errors.New("permanent fail")
		err := Retry(context.Background(), func() error {
			calls++
			return targetErr
		}, WithAttempts(3), WithDelay(1*time.Millisecond, 1*time.Millisecond))

		if err != targetErr || calls != 3 {
			t.Errorf("Retry should return last error: %v, calls=%d", err, calls)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		calls := 0
		err := Retry(ctx, func() error {
			calls++
			return errors.New("fail")
		})

		if err != context.Canceled || calls != 0 {
			t.Errorf("Retry should stop on context cancel: %v", err)
		}
	})
}

func TestRetryRejectsInvalidOptions(t *testing.T) {
	calls := 0
	fn := func() error {
		calls++
		return nil
	}
	tests := []func(*RetryOptions){
		WithAttempts(0),
		WithDelay(-time.Second, time.Second),
		WithDelay(time.Second, 0),
		WithFactor(0),
		WithJitter(2),
	}
	for _, option := range tests {
		if err := Retry(context.Background(), fn, option); err == nil {
			t.Fatal("Retry() accepted invalid options")
		}
	}
	if calls != 0 {
		t.Fatalf("Retry() called function %d times for invalid options", calls)
	}
}
