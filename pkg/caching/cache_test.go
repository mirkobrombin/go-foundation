package caching

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryCacheSetGetInvalidate(t *testing.T) {
	cache := NewInMemory[string]()
	ctx := context.Background()

	if err := cache.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok, err := cache.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got != "value" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}

	if err := cache.Invalidate(ctx, "key"); err != nil {
		t.Fatalf("Invalidate() error = %v", err)
	}
	if _, ok, _ := cache.Get(ctx, "key"); ok {
		t.Fatal("Get() found invalidated key")
	}
}

func TestInMemoryCacheTTL(t *testing.T) {
	cache := NewInMemory[string](WithTTL[string](time.Nanosecond))
	ctx := context.Background()

	if err := cache.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(time.Millisecond)

	if _, ok, _ := cache.Get(ctx, "key"); ok {
		t.Fatal("Get() found expired key")
	}
}

func TestInMemoryCacheMaxEntries(t *testing.T) {
	cache := NewInMemory[string](WithMaxEntries[string](1))
	ctx := context.Background()

	cache.Set(ctx, "a", "A", 0)
	cache.Set(ctx, "b", "B", 0)

	if cache.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", cache.Len())
	}
}
