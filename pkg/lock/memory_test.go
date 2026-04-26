package lock

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestInMemoryLocker_TryLock(t *testing.T) {
	l := NewInMemoryLocker()
	ok, err := l.TryLock(context.Background(), "key1", 0)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected lock acquired")
	}

	ok, err = l.TryLock(context.Background(), "key1", 0)
	if err != nil {
		t.Fatalf("TryLock second call: %v", err)
	}
	if ok {
		t.Fatal("expected lock not acquired (already held)")
	}
}

func TestInMemoryLocker_Release(t *testing.T) {
	l := NewInMemoryLocker()
	l.TryLock(context.Background(), "k", 0)

	err := l.Release(context.Background(), "k")
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	ok, err := l.TryLock(context.Background(), "k", 0)
	if err != nil {
		t.Fatalf("TryLock after release: %v", err)
	}
	if !ok {
		t.Fatal("expected lock acquired after release")
	}
}

func TestInMemoryLocker_Acquire(t *testing.T) {
	l := NewInMemoryLocker()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		l.TryLock(context.Background(), "shared", 0)
		time.Sleep(50 * time.Millisecond)
		l.Release(context.Background(), "shared")
	}()

	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := l.Acquire(ctx, "shared", 0)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	wg.Wait()
}

func TestInMemoryLocker_AcquireContextCancel(t *testing.T) {
	l := NewInMemoryLocker()
	l.TryLock(context.Background(), "blocked", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := l.Acquire(ctx, "blocked", 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestInMemoryLocker_DifferentKeys(t *testing.T) {
	l := NewInMemoryLocker()
	ok1, _ := l.TryLock(context.Background(), "a", 0)
	ok2, _ := l.TryLock(context.Background(), "b", 0)
	if !ok1 || !ok2 {
		t.Fatal("different keys should not conflict")
	}
}
