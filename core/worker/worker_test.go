package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/worker"
)

func TestPoolExecutesSubmittedTasks(t *testing.T) {
	pool := worker.NewPool(3)
	defer pool.Shutdown()

	var count int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		ok := pool.Submit(func(ctx context.Context) error {
			defer wg.Done()
			atomic.AddInt32(&count, 1)
			return nil
		})
		if !ok {
			t.Fatalf("Submit() = false, want true")
		}
	}

	wg.Wait()
	if count != 10 {
		t.Fatalf("executed tasks = %d, want %d", count, 10)
	}
}

func TestPoolRejectsTasksAfterShutdown(t *testing.T) {
	pool := worker.NewPool(1)
	pool.Shutdown()

	if ok := pool.Submit(func(ctx context.Context) error { return nil }); ok {
		t.Fatalf("Submit() after Shutdown() = true, want false")
	}
}

func TestPoolRejectsNilTask(t *testing.T) {
	pool := worker.NewPool(1)
	defer pool.Shutdown()

	if pool.Submit(nil) {
		t.Fatal("Submit(nil) = true, want false")
	}
}

func TestPoolDoesNotStartTasksAfterShutdownBegins(t *testing.T) {
	for range 100 {
		pool := worker.NewPool(1)
		started := make(chan struct{})
		shutdownBegan := make(chan struct{})
		release := make(chan struct{})
		if !pool.Submit(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(shutdownBegan)
			<-release
			return nil
		}) {
			t.Fatal("initial Submit() rejected task")
		}
		<-started

		var executed atomic.Bool
		submitted := make(chan bool, 1)
		go func() {
			submitted <- pool.Submit(func(context.Context) error {
				executed.Store(true)
				return nil
			})
		}()
		shutdown := make(chan struct{})
		go func() {
			pool.Shutdown()
			close(shutdown)
		}()
		<-shutdownBegan
		close(release)

		if <-submitted {
			t.Fatal("Submit() accepted task after shutdown began")
		}
		<-shutdown
		if executed.Load() {
			t.Fatal("task executed after shutdown began")
		}
	}
}

func TestPoolDefaultsToOneWorker(t *testing.T) {
	pool := worker.NewPool(0)

	var count int32
	ok := pool.Submit(func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})
	if !ok {
		t.Fatalf("Submit() = false, want true")
	}

	pool.Shutdown()
	if count != 1 {
		t.Fatalf("executed tasks = %d, want %d", count, 1)
	}
}

func TestPoolShutdownCancelsRunningTasks(t *testing.T) {
	pool := worker.NewPool(1)
	started := make(chan struct{})
	if !pool.Submit(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}) {
		t.Fatal("Submit() rejected task")
	}
	<-started

	done := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Shutdown() did not cancel the running task")
	}
}

func TestPoolSurvivesTaskPanic(t *testing.T) {
	pool := worker.NewPool(1)
	defer pool.Shutdown()
	if !pool.Submit(func(context.Context) error {
		panic("task failed")
	}) {
		t.Fatal("Submit() rejected panic task")
	}

	done := make(chan struct{})
	if !pool.Submit(func(context.Context) error {
		close(done)
		return nil
	}) {
		t.Fatal("Submit() rejected task after panic")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker exited after task panic")
	}
}
