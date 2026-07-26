package lock

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestInMemoryLocker_TryLock(t *testing.T) {
	l := NewInMemoryLocker()
	lease, ok, err := l.TryLock(context.Background(), "key1", 0)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected lock acquired")
	}
	defer lease.Release(context.Background())

	_, ok, err = l.TryLock(context.Background(), "key1", 0)
	if err != nil {
		t.Fatalf("TryLock second call: %v", err)
	}
	if ok {
		t.Fatal("expected lock not acquired (already held)")
	}
}

func TestInMemoryLocker_Release(t *testing.T) {
	l := NewInMemoryLocker()
	lease, _, _ := l.TryLock(context.Background(), "k", 0)

	err := lease.Release(context.Background())
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	_, ok, err := l.TryLock(context.Background(), "k", 0)
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
		lease, _, _ := l.TryLock(context.Background(), "shared", 0)
		time.Sleep(50 * time.Millisecond)
		lease.Release(context.Background())
	}()

	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := l.Acquire(ctx, "shared", 0)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	wg.Wait()
}

func TestInMemoryLocker_AcquireContextCancel(t *testing.T) {
	l := NewInMemoryLocker()
	lease, _, _ := l.TryLock(context.Background(), "blocked", 0)
	defer lease.Release(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := l.Acquire(ctx, "blocked", 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestInMemoryLocker_DifferentKeys(t *testing.T) {
	l := NewInMemoryLocker()
	lease1, ok1, _ := l.TryLock(context.Background(), "a", 0)
	lease2, ok2, _ := l.TryLock(context.Background(), "b", 0)
	defer lease1.Release(context.Background())
	defer lease2.Release(context.Background())
	if !ok1 || !ok2 {
		t.Fatal("different keys should not conflict")
	}
}

func TestInMemoryLocker_TTLReleasesLock(t *testing.T) {
	l := NewInMemoryLocker()
	_, ok, err := l.TryLock(context.Background(), "expiring", 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("TryLock() = %v, %v, want true, nil", ok, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := l.Acquire(ctx, "expiring", 0)
	if err != nil {
		t.Fatalf("Acquire() after TTL: %v", err)
	}
	defer lease.Release(context.Background())
}

func TestInMemoryLocker_ExpiredOwnerDoesNotReleaseNewLock(t *testing.T) {
	l := NewInMemoryLocker()
	first, ok, err := l.TryLock(context.Background(), "reused", 40*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("first TryLock() = %v, %v", ok, err)
	}
	if err := first.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, ok, err := l.TryLock(context.Background(), "reused", 0)
	if err != nil || !ok {
		t.Fatalf("second TryLock() = %v, %v", ok, err)
	}
	if err := first.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	_, ok, err = l.TryLock(context.Background(), "reused", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expired owner released a newer lock")
	}
	if err := second.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}
