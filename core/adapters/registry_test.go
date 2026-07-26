package adapters

import (
	"testing"
	"time"
)

type mockTransport struct {
	Name string
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry[*mockTransport]()
	tr := &mockTransport{Name: "http"}
	r.Register("http", tr)

	got, ok := r.Get("http")
	if !ok {
		t.Fatal("expected to find 'http'")
	}
	if got.Name != "http" {
		t.Errorf("got %q, want %q", got.Name, "http")
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry[string]()
	r.Register("exists", "value")

	if !r.Has("exists") {
		t.Error("Has should return true")
	}
	if r.Has("missing") {
		t.Error("Has should return false for missing")
	}
}

func TestRegistry_Default(t *testing.T) {
	r := NewRegistry[string]()
	r.Register("http", "HTTP Transport")
	r.Register("grpc", "gRPC Transport")
	r.SetDefault("http")

	got := r.Default()
	if got != "HTTP Transport" {
		t.Errorf("got %q, want %q", got, "HTTP Transport")
	}
}

func TestRegistry_DefaultOr(t *testing.T) {
	r := NewRegistry[string]()

	got := r.DefaultOr("fallback")
	if got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}

	r.Register("main", "Main")
	r.SetDefault("main")

	got = r.DefaultOr("fallback")
	if got != "Main" {
		t.Errorf("got %q, want %q", got, "Main")
	}
}

func TestRegistry_Default_Panic(t *testing.T) {
	r := NewRegistry[string]()

	defer func() {
		if recover() == nil {
			t.Error("Default should panic when no default set")
		}
	}()

	r.Default()
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry[int]()
	r.Register("a", 1)
	r.Register("b", 2)
	r.Register("c", 3)

	names := r.Names()
	if len(names) != 3 {
		t.Errorf("got %d names, want 3", len(names))
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := NewRegistry[string]()
	r.Register("key", "value")
	r.SetDefault("key")

	r.Remove("key")

	if r.Has("key") {
		t.Error("key should be removed")
	}

	defer func() {
		if recover() == nil {
			t.Error("Default should panic after removing default")
		}
	}()
	r.Default()
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry[int]()
	r.Register("a", 1)
	r.Register("b", 2)
	r.SetDefault("a")

	r.Clear()

	if len(r.Names()) != 0 {
		t.Error("Clear should remove all adapters")
	}
}

func TestRegistry_MustGet_Panic(t *testing.T) {
	r := NewRegistry[string]()

	defer func() {
		if recover() == nil {
			t.Error("MustGet should panic for missing key")
		}
	}()

	r.MustGet("missing")
}

func TestRegistryCallbacksCanReenter(t *testing.T) {
	registry := NewRegistry[int]()
	registry.OnRegister(func(name string, value int) {
		if got, ok := registry.Get(name); !ok || got != value {
			t.Errorf("Get() in OnRegister = %d, %v", got, ok)
		}
	})
	registry.OnRemove(func(name string) {
		if registry.Has(name) {
			t.Errorf("Has(%q) in OnRemove = true", name)
		}
	})

	done := make(chan struct{})
	go func() {
		registry.Register("value", 1)
		registry.Remove("value")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registry callback deadlocked")
	}
}
