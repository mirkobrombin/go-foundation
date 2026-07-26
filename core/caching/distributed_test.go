package caching

import (
	"context"
	"testing"
	"time"
)

type cachedUser struct {
	Name string `json:"name"`
}

func TestDistributedBridgeRoundTrip(t *testing.T) {
	ctx := context.Background()
	backend := NewDistributedInMemory()
	cache := NewDistributedBridge[cachedUser](backend)

	if err := cache.Set(ctx, "user", cachedUser{Name: "alice"}, 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok, err := cache.Get(ctx, "user")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got.Name != "alice" {
		t.Fatalf("Get() = %+v, %v", got, ok)
	}

	if err := cache.Invalidate(ctx, "user"); err != nil {
		t.Fatalf("Invalidate() error = %v", err)
	}
	if _, ok, _ := cache.Get(ctx, "user"); ok {
		t.Fatal("Get() found invalidated key")
	}
}

func TestDistributedInMemoryTTL(t *testing.T) {
	ctx := context.Background()
	backend := NewDistributedInMemory(WithDistributedTTL(time.Nanosecond))

	if err := backend.Set(ctx, "key", []byte("value"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(time.Millisecond)

	if _, ok, _ := backend.Get(ctx, "key"); ok {
		t.Fatal("Get() found expired key")
	}
}

func TestDistributedInMemoryCopiesValues(t *testing.T) {
	ctx := context.Background()
	backend := NewDistributedInMemory()
	value := []byte("value")
	if err := backend.Set(ctx, "key", value, 0); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'

	got, ok, err := backend.Get(ctx, "key")
	if err != nil || !ok || string(got) != "value" {
		t.Fatalf("Get() = %q, %v, %v", got, ok, err)
	}
	got[0] = 'Y'
	again, _, _ := backend.Get(ctx, "key")
	if string(again) != "value" {
		t.Fatalf("caller mutated cached data: %q", again)
	}
}

func TestDistributedInMemoryMaxEntries(t *testing.T) {
	ctx := context.Background()
	backend := NewDistributedInMemory(WithDistributedMaxEntries(1))
	if err := backend.Set(ctx, "one", []byte("1"), 0); err != nil {
		t.Fatal(err)
	}
	if err := backend.Set(ctx, "two", []byte("2"), 0); err != nil {
		t.Fatal(err)
	}
	backend.mu.RLock()
	size := len(backend.data)
	backend.mu.RUnlock()
	if size != 1 {
		t.Fatalf("entry count = %d, want 1", size)
	}
}
