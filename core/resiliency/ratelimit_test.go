package resiliency

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRateLimiterRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		rate  int
		burst int
	}{
		{rate: 0, burst: 1},
		{rate: 1, burst: 0},
		{rate: -1, burst: 1},
	} {
		if _, err := NewRateLimiter(test.rate, test.burst); err == nil {
			t.Fatalf("NewRateLimiter(%d, %d) accepted invalid values", test.rate, test.burst)
		}
	}
}

func TestBulkheadEnforcesConcurrencyAndQueueLimits(t *testing.T) {
	bulkhead, err := NewBulkhead(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var running atomic.Int32
	var peak atomic.Int32
	run := func() error {
		current := running.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		running.Add(-1)
		return nil
	}

	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- bulkhead.Execute(context.Background(), run) }()
	<-started
	go func() { second <- bulkhead.Execute(context.Background(), run) }()

	deadline := time.Now().Add(time.Second)
	for len(bulkhead.queue) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := bulkhead.Execute(context.Background(), run); !errors.Is(err, ErrBulkheadFull) {
		t.Fatalf("third Execute() error = %v, want ErrBulkheadFull", err)
	}

	release <- struct{}{}
	<-started
	release <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("peak concurrency = %d, want 1", got)
	}
}

func TestNewBulkheadRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewBulkhead(0, 1); err == nil {
		t.Fatal("NewBulkhead() accepted zero concurrency")
	}
	if _, err := NewBulkhead(1, -1); err == nil {
		t.Fatal("NewBulkhead() accepted a negative queue")
	}
}
