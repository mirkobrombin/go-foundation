package resiliency

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)

	// State: Closed
	err := cb.Execute(func() error { return nil })
	if err != nil || cb.State() != StateClosed {
		t.Error("Circuit should be closed and return nil")
	}

	// First failure
	_ = cb.Execute(func() error { return errors.New("fail1") })
	if cb.State() != StateClosed {
		t.Error("Circuit should still be closed after 1 failure")
	}

	// Second failure -> State: Open
	_ = cb.Execute(func() error { return errors.New("fail2") })
	if cb.State() != StateOpen {
		t.Error("Circuit should be open after reaching threshold")
	}

	// While Open
	err = cb.Execute(func() error { return nil })
	if err != ErrCircuitOpen {
		t.Errorf("Execute should return ErrCircuitOpen when open, got %v", err)
	}

	// Wait for timeout -> State: Half-Open (on next Execute)
	time.Sleep(60 * time.Millisecond)

	// First success in Half-Open -> State: Closed
	err = cb.Execute(func() error { return nil })
	if err != nil || cb.State() != StateClosed {
		t.Errorf("Circuit should be closed after success in half-open, got state %v", cb.State())
	}
}

// TestHalfOpenAllowsOnlyOneProbe verifies that in HalfOpen state only one
// concurrent request is allowed through; all others receive ErrCircuitOpen.
func TestHalfOpenAllowsOnlyOneProbe(t *testing.T) {
	const timeout = 30 * time.Millisecond
	cb := NewCircuitBreaker(1, timeout)

	// Trip the breaker.
	_ = cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != StateOpen {
		t.Fatal("expected StateOpen after threshold reached")
	}

	// Wait for the open timeout to elapse so the next allow() transitions to HalfOpen.
	time.Sleep(timeout + 10*time.Millisecond)

	// Fire 5 concurrent requests; use a gate so they all call allow() at the same time.
	const n = 5
	var (
		wg      sync.WaitGroup
		gate    = make(chan struct{})
		allowed int32
		blocked int32
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			err := cb.Execute(func() error {
				// Slow probe so concurrent goroutines can observe HalfOpen.
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			if err == ErrCircuitOpen {
				atomic.AddInt32(&blocked, 1)
			} else if err == nil {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}

	close(gate)
	wg.Wait()

	if allowed != 1 {
		t.Errorf("expected exactly 1 probe allowed, got %d", allowed)
	}
	if blocked != n-1 {
		t.Errorf("expected %d blocked, got %d", n-1, blocked)
	}
	if cb.State() != StateClosed {
		t.Errorf("expected StateClosed after successful probe, got %v", cb.State())
	}
}

// TestHalfOpenFailureResetsCounter verifies that after a HalfOpen→Open cycle
// the failure counter is reset, so a fresh probe failure in the new HalfOpen
// correctly re-opens without a stale count.
func TestHalfOpenFailureResetsCounter(t *testing.T) {
	const timeout = 30 * time.Millisecond
	cb := NewCircuitBreaker(2, timeout)

	// Reach threshold to open breaker (2 failures).
	_ = cb.Execute(func() error { return errors.New("f1") })
	_ = cb.Execute(func() error { return errors.New("f2") })
	if cb.State() != StateOpen {
		t.Fatal("expected StateOpen")
	}

	// First HalfOpen → probe fails → back to Open.
	time.Sleep(timeout + 10*time.Millisecond)
	_ = cb.Execute(func() error { return errors.New("probe fail") })
	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after half-open probe failure, got %v", cb.State())
	}

	// Second HalfOpen: failures counter must have been reset to 0 on the previous
	// HalfOpen→Open transition.  A single failure in this new HalfOpen should
	// send the breaker back to Open immediately (not leave it closed/half-open).
	time.Sleep(timeout + 10*time.Millisecond)
	_ = cb.Execute(func() error { return errors.New("probe fail 2") })
	if cb.State() != StateOpen {
		t.Errorf("expected StateOpen after second half-open failure, got %v", cb.State())
	}
}
