package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestScheduler_RegisterAndFire(t *testing.T) {
	s := New()
	var mu sync.Mutex
	fired := false

	s.Register(Job{
		Name:    "test",
		Cron:    "* * * * *",
		Handler: func(ctx context.Context) error { mu.Lock(); fired = true; mu.Unlock(); return nil },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)

	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()
	if !running {
		t.Error("scheduler should be running")
	}

	mu.Lock()
	wasFired := fired
	mu.Unlock()
	if !wasFired {
		t.Error("job should have fired within timeout")
	}
}

func TestScheduler_DoubleStart(t *testing.T) {
	s := New()
	ctx := context.Background()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	if err := s.Start(ctx); err == nil {
		t.Error("double start should return an error")
	}
}

func TestScheduler_StopNotRunning(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.Stop(ctx); err == nil {
		t.Error("stopping non-running scheduler should return an error")
	}
}

func TestScheduler_Enqueue(t *testing.T) {
	s := New()
	var mu sync.Mutex
	called := false

	s.Enqueue(func(ctx context.Context) error {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	wasCalled := called
	mu.Unlock()
	if !wasCalled {
		t.Error("enqueued function should have been called")
	}
}

func TestScheduler_ScheduleAfter(t *testing.T) {
	s := New()
	var mu sync.Mutex
	called := false

	s.ScheduleAfter(50*time.Millisecond, func(ctx context.Context) error {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	wasCalled := called
	mu.Unlock()
	if !wasCalled {
		t.Error("scheduled function should have been called after delay")
	}
}