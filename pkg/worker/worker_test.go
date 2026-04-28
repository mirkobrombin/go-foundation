package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/worker"
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
