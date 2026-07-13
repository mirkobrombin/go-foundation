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
