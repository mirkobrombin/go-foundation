package events

import (
	"context"
	"sync"
	"testing"
)

func TestBus_SubscribeAndEmit(t *testing.T) {
	b := New()

	var received MyEvent
	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		received = e
		return nil
	})

	err := Emit(context.Background(), b, MyEvent{ID: 1})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if received.ID != 1 {
		t.Errorf("received %d, want 1", received.ID)
	}
}

type MyEvent struct{ ID int }

func TestBus_DefaultBus(t *testing.T) {
	defaultBus = New()

	var received MyEvent
	Subscribe(Default(), func(ctx context.Context, e MyEvent) error {
		received = e
		return nil
	})

	Emit(context.Background(), Default(), MyEvent{ID: 2})
	if received.ID != 2 {
		t.Errorf("received %d, want 2", received.ID)
	}
}

func TestBus_NilBusDefaults(t *testing.T) {
	b := New()
	defer b.Close()

	var received MyEvent
	Subscribe(nil, func(ctx context.Context, e MyEvent) error {
		received = e
		return nil
	})

	Emit(context.Background(), nil, MyEvent{ID: 8})
	if received.ID != 8 {
		t.Errorf("received %d, want 8", received.ID)
	}
}

func TestBus_EmitAsync(t *testing.T) {
	b := New()
	defer b.Close()

	var mu sync.Mutex
	var received []MyEvent
	done := make(chan struct{})

	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, e)
		if len(received) == 5 {
			close(done)
		}
		return nil
	})

	for i := 0; i < 5; i++ {
		EmitAsync(context.Background(), b, MyEvent{ID: i})
	}

	select {
	case <-done:
	case <-b.asyncClose:
		t.Fatal("bus closed before events processed")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 5 {
		t.Fatalf("got %d events, want 5", len(received))
	}
}

func TestBus_Middleware(t *testing.T) {
	b := New()
	defer b.Close()

	var chain []string
	b.Use(func(ctx context.Context, event any, next func(ctx context.Context, event any) error) error {
		chain = append(chain, "before")
		err := next(ctx, event)
		chain = append(chain, "after")
		return err
	})

	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		chain = append(chain, "handler")
		return nil
	})

	Emit(context.Background(), b, MyEvent{ID: 3})

	if len(chain) != 3 || chain[0] != "before" || chain[1] != "handler" || chain[2] != "after" {
		t.Errorf("middleware chain wrong: %v", chain)
	}
}

func TestBus_StrategyStopOnFirstError(t *testing.T) {
	b := New()
	defer b.Close()

	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		return context.Canceled
	})
	called := false
	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		called = true
		return nil
	})

	err := Emit(context.Background(), b, MyEvent{ID: 5})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if called {
		t.Error("second handler should not be called under StopOnFirstError")
	}
}

func TestBus_OnAsyncError(t *testing.T) {
	b := New(
		WithStrategy(StopOnFirstError),
		WithOnAsyncError(func(err error) {
			if err == nil {
				t.Error("expected error in async handler")
			}
		}),
	)
	defer b.Close()

	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		return context.Canceled
	})

	EmitAsync(context.Background(), b, MyEvent{ID: 5})
}

func TestBus_Close(t *testing.T) {
	b := New()
	b.Close()
}

func TestBus_EmitNoSubscribers(t *testing.T) {
	b := New()
	defer b.Close()
	err := Emit(context.Background(), b, MyEvent{ID: 7})
	if err != nil {
		t.Errorf("Emit without subscribers: %v", err)
	}
}

func TestBus_NilBusDefault(t *testing.T) {
	var b *Bus = nil
	Subscribe(b, func(ctx context.Context, e MyEvent) error {
		return nil
	})
	err := Emit(context.Background(), b, MyEvent{ID: 8})
	if err != nil {
		t.Errorf("Emit with nil bus: %v", err)
	}
}
