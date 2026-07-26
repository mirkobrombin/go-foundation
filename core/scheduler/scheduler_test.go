package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScheduler_RegisterAndFire(t *testing.T) {
	s := New()
	var mu sync.Mutex
	fired := false

	if err := s.Register(Job{
		Name:    "test",
		Cron:    "* * * * *",
		Handler: func(ctx context.Context) error { mu.Lock(); fired = true; mu.Unlock(); return nil },
	}); err != nil {
		t.Fatal(err)
	}

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

func TestScheduler_RejectsInvalidJobs(t *testing.T) {
	tests := []Job{
		{Name: "", Cron: "* * * * *", Handler: func(context.Context) error { return nil }},
		{Name: "nil", Cron: "* * * * *"},
		{Name: "bad", Cron: "bogus * * * *", Handler: func(context.Context) error { return nil }},
		{Name: "range", Cron: "60 * * * *", Handler: func(context.Context) error { return nil }},
	}
	for _, job := range tests {
		scheduler := New()
		if err := scheduler.Register(job); err == nil {
			t.Fatalf("Register() accepted job %#v", job)
		}
	}

	scheduler := New()
	job := Job{Name: "duplicate", Cron: "* * * * *", Handler: func(context.Context) error { return nil }}
	if err := scheduler.Register(job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Register(job); err == nil {
		t.Fatal("Register() accepted a duplicate name")
	}
}

func TestScheduler_DoesNotRunFutureCronImmediately(t *testing.T) {
	now := time.Date(2026, time.July, 26, 12, 30, 0, 0, time.UTC)
	if isDue("0 0 * * *", now, time.Time{}) {
		t.Fatal("future cron was due only because it had no previous run")
	}
}

func TestScheduler_StopNotRunning(t *testing.T) {
	s := New()
	ctx := context.Background()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop() should be idempotent: %v", err)
	}
}

func TestScheduler_StopWaitsForInflightTask(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler.Enqueue(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	<-started

	stopped := make(chan error, 1)
	go func() {
		stopped <- scheduler.Stop(context.Background())
	}()
	select {
	case err := <-stopped:
		t.Fatalf("Stop() returned before the task completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestScheduler_RejectsRestartWhileTimedOutStopIsPending(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler.Enqueue(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	<-started

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := scheduler.Stop(stopCtx); err == nil {
		t.Fatal("Stop() ignored its deadline")
	}
	if err := scheduler.Start(context.Background()); err == nil {
		t.Fatal("Start() restarted while the previous Stop was incomplete")
	}

	close(release)
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("Start() after completed Stop error = %v", err)
	}
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
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

func TestSchedulerRecoversAsyncHandlerPanics(t *testing.T) {
	messages := make(chan string, 1)
	scheduler := New(WithLogger(func(message string) {
		if strings.Contains(message, "panic") {
			messages <- message
		}
	}))
	scheduler.Enqueue(func(context.Context) error {
		panic("job failed")
	})

	select {
	case message := <-messages:
		if !strings.Contains(message, "job failed") {
			t.Fatalf("panic log = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not report recovered panic")
	}
}
