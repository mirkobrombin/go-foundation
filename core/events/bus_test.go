package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestBus_EmitAny(t *testing.T) {
	b := New()
	defer b.Close()

	var received *MyEvent
	Subscribe(b, func(ctx context.Context, e *MyEvent) error {
		received = e
		return nil
	})

	err := EmitAny(context.Background(), b, &MyEvent{ID: 9})
	if err != nil {
		t.Fatalf("EmitAny: %v", err)
	}
	if received == nil || received.ID != 9 {
		t.Fatalf("received = %#v", received)
	}
}

func TestBus_EmitAnyWithMiddlewareAndWildcard(t *testing.T) {
	b := New()
	defer b.Close()

	b.Use(func(ctx context.Context, event any, next func(context.Context, any) error) error {
		return next(ctx, event)
	})

	called := false
	SubscribeWildcard(b, func(ctx context.Context, event any) error {
		called = true
		return nil
	})

	err := EmitAny(context.Background(), b, &MyEvent{ID: 10})
	if err != nil {
		t.Fatalf("EmitAny: %v", err)
	}
	if !called {
		t.Fatal("wildcard handler was not called")
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

func TestBus_EmitAsyncUsesMiddlewareAndWildcard(t *testing.T) {
	b := New()
	defer b.Close()

	middlewareCalled := make(chan struct{}, 1)
	wildcardCalled := make(chan struct{}, 1)
	b.Use(func(ctx context.Context, event any, next func(context.Context, any) error) error {
		middlewareCalled <- struct{}{}
		return next(ctx, event)
	})
	SubscribeWildcard(b, func(context.Context, any) error {
		wildcardCalled <- struct{}{}
		return nil
	})
	Subscribe(b, func(context.Context, MyEvent) error {
		return nil
	})

	EmitAsync(context.Background(), b, MyEvent{ID: 6})

	for name, called := range map[string]<-chan struct{}{
		"middleware": middlewareCalled,
		"wildcard":   wildcardCalled,
	} {
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for async %s", name)
		}
	}
}

func TestBus_EmitAnyAsyncReportsBestEffortErrors(t *testing.T) {
	firstErr := errors.New("first")
	secondErr := errors.New("second")
	asyncErr := make(chan error, 1)
	b := New(
		WithStrategy(BestEffort),
		WithOnAsyncError(func(err error) {
			asyncErr <- err
		}),
	)
	defer b.Close()

	Subscribe(b, func(context.Context, MyEvent) error {
		return firstErr
	})
	Subscribe(b, func(context.Context, MyEvent) error {
		return secondErr
	})

	EmitAnyAsync(context.Background(), b, MyEvent{ID: 7})

	select {
	case err := <-asyncErr:
		if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
			t.Fatalf("async error = %v, want both handler errors", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async error")
	}
}

func TestBus_Close(t *testing.T) {
	b := New()
	b.Close()
	b.Close()
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		EmitAsync(context.Background(), b, MyEvent{ID: 1})
		close(done)
	}()
	<-done
}

func TestBus_CloseDrainsQueuedEvents(t *testing.T) {
	b := New(WithBufferSize(16))
	var handled int
	var mu sync.Mutex
	Subscribe(b, func(context.Context, MyEvent) error {
		mu.Lock()
		handled++
		mu.Unlock()
		return nil
	})
	for index := 0; index < 10; index++ {
		EmitAsync(context.Background(), b, MyEvent{ID: index})
	}

	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if handled != 10 {
		t.Fatalf("Shutdown() drained %d events, want 10", handled)
	}
}

func TestBus_AsyncHandlerCanCloseBus(t *testing.T) {
	b := New(WithBufferSize(1))
	handlerDone := make(chan struct{})
	Subscribe(b, func(context.Context, MyEvent) error {
		b.Close()
		close(handlerDone)
		return nil
	})
	EmitAsync(context.Background(), b, MyEvent{})

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("async handler deadlocked in Close()")
	}
	select {
	case <-b.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("bus did not finish shutdown after reentrant Close()")
	}
}

func TestBus_AsyncHandlerCannotWaitForOwnShutdown(t *testing.T) {
	b := New()
	handlerDone := make(chan error, 1)
	Subscribe(b, func(ctx context.Context, _ MyEvent) error {
		handlerDone <- b.Shutdown(ctx)
		return nil
	})
	EmitAsync(context.Background(), b, MyEvent{})

	select {
	case err := <-handlerDone:
		if !errors.Is(err, ErrReentrantShutdown) {
			t.Fatalf("Shutdown() error = %v, want ErrReentrantShutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("async handler deadlocked in Shutdown()")
	}
	select {
	case <-b.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("bus did not finish after reentrant Shutdown()")
	}
}

func TestBus_AsyncHandlerCanEmitAsync(t *testing.T) {
	emitErr := make(chan error, 1)
	b := New(WithBufferSize(1))
	Subscribe(b, func(ctx context.Context, _ MyEvent) error {
		emitErr <- EmitAsync(ctx, b, 1)
		return nil
	})
	if err := EmitAsync(context.Background(), b, MyEvent{}); err != nil {
		t.Fatalf("EmitAsync: %v", err)
	}

	select {
	case err := <-emitErr:
		if err != nil {
			t.Fatalf("reentrant emission error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("async handler deadlocked while emitting")
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBus_DropOldestDoesNotBlockReentrantEmission(t *testing.T) {
	emitErr := make(chan error, 1)
	b := New(
		WithBufferSize(1),
		WithOverflowStrategy(OverflowDropOldest),
	)
	Subscribe(b, func(ctx context.Context, _ MyEvent) error {
		if err := EmitAsync(ctx, b, 1); err != nil {
			emitErr <- err
			return nil
		}
		emitErr <- EmitAsync(ctx, b, 2)
		return nil
	})
	if err := EmitAsync(context.Background(), b, MyEvent{}); err != nil {
		t.Fatalf("EmitAsync: %v", err)
	}

	select {
	case err := <-emitErr:
		if err != nil {
			t.Fatalf("reentrant emission error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DropOldest deadlocked on a reentrant unbuffered emission")
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBus_AsyncErrorCallbackCanEmitToSameBus(t *testing.T) {
	emitErr := make(chan error, 1)
	var b *Bus
	b = New(
		WithBufferSize(1),
		WithOnAsyncError(func(error) {
			if err := EmitAsync(context.Background(), b, 1); err != nil {
				emitErr <- err
				return
			}
			emitErr <- EmitAsync(context.Background(), b, 2)
		}),
	)
	Subscribe(b, func(context.Context, MyEvent) error {
		return errors.New("handler failed")
	})
	if err := EmitAsync(context.Background(), b, MyEvent{}); err != nil {
		t.Fatalf("EmitAsync: %v", err)
	}

	select {
	case err := <-emitErr:
		if !errors.Is(err, ErrAsyncQueueFull) {
			t.Fatalf("callback emission error = %v, want ErrAsyncQueueFull", err)
		}
	case <-time.After(time.Second):
		t.Fatal("async error callback deadlocked while emitting to the same bus")
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBus_AsyncPanicBecomesError(t *testing.T) {
	asyncErr := make(chan error, 1)
	b := New(WithOnAsyncError(func(err error) {
		asyncErr <- err
	}))
	defer b.Close()
	Subscribe(b, func(context.Context, MyEvent) error {
		panic("handler failed")
	})

	EmitAsync(context.Background(), b, MyEvent{})

	select {
	case err := <-asyncErr:
		if err == nil || !strings.Contains(err.Error(), "handler failed") {
			t.Fatalf("async panic error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovered panic")
	}
}

func TestBus_CustomBufferProcessesEvents(t *testing.T) {
	b := New(WithBufferSize(1))
	defer b.Close()
	received := make(chan struct{}, 1)
	Subscribe(b, func(context.Context, MyEvent) error {
		received <- struct{}{}
		return nil
	})
	EmitAsync(context.Background(), b, MyEvent{ID: 1})
	<-received
}

func TestBus_EmitAsyncPreservesContext(t *testing.T) {
	type contextKey struct{}
	bus := New()
	defer bus.Close()
	received := make(chan string, 1)
	Subscribe(bus, func(ctx context.Context, event int) error {
		value, _ := ctx.Value(contextKey{}).(string)
		received <- value
		return nil
	})
	ctx := context.WithValue(context.Background(), contextKey{}, "tenant")
	EmitAsync(ctx, bus, 1)

	select {
	case value := <-received:
		if value != "tenant" {
			t.Fatalf("context value = %q, want tenant", value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async event")
	}
}

func TestBus_RejectsInvalidBufferSize(t *testing.T) {
	for _, size := range []int{-1, 0} {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New() accepted an invalid async buffer")
				}
			}()
			New(WithBufferSize(size))
		})
	}
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
