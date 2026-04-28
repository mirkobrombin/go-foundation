package manager

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/relay/broker"
)

type testPayload struct {
	Message string `json:"message"`
	Value   int    `json:"value"`
}

func TestNew_DefaultBroker(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New returned nil")
	}
}

func TestRegisterAndEnqueue(t *testing.T) {
	r := New()
	var mu sync.Mutex
	var received testPayload
	handlerCalled := make(chan struct{})

	Register[testPayload](r, "test.topic", func(ctx context.Context, p testPayload) error {
		mu.Lock()
		received = p
		mu.Unlock()
		close(handlerCalled)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, err := r.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-ready

	Enqueue[testPayload](context.Background(), r, "test.topic", testPayload{Message: "hello", Value: 42})

	select {
	case <-handlerCalled:
		mu.Lock()
		if received.Message != "hello" || received.Value != 42 {
			t.Errorf("got %+v, want {Message:hello Value:42}", received)
		}
		mu.Unlock()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler")
	}
}

func TestRegister_TypeSafety(t *testing.T) {
	r := New()

	Register[testPayload](r, "topic1", func(ctx context.Context, p testPayload) error {
		return nil
	})
	Register[int](r, "topic2", func(ctx context.Context, p int) error {
		return nil
	})

	if len(r.handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(r.handlers))
	}
}

func TestEnqueue_MarshalError(t *testing.T) {
	r := New()

	err := Enqueue[testPayload](context.Background(), r, "test", testPayload{})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
}

func TestBroker_Interface(t *testing.T) {
	b := broker.NewMemoryBroker()
	r := New(WithBroker(b))
	if r.broker != b {
		t.Error("broker not set correctly")
	}
}

func TestStart_ContextCancel(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ready, err := r.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-ready
}

func TestMultipleTopics(t *testing.T) {
	r := New()
	ch1 := make(chan int, 1)
	ch2 := make(chan int, 1)

	Register[int](r, "topic.a", func(ctx context.Context, p int) error {
		ch1 <- p
		return nil
	})
	Register[int](r, "topic.b", func(ctx context.Context, p int) error {
		ch2 <- p
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, err := r.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-ready

	Enqueue[int](context.Background(), r, "topic.a", 1)
	Enqueue[int](context.Background(), r, "topic.b", 2)

	var results [2]int
	select {
	case results[0] = <-ch1:
	case <-time.After(time.Second):
		t.Fatal("timeout for topic.a")
	}
	select {
	case results[1] = <-ch2:
	case <-time.After(time.Second):
		t.Fatal("timeout for topic.b")
	}

	if results[0] != 1 || results[1] != 2 {
		t.Errorf("got %v, want [1 2]", results[:])
	}
}

func TestEnqueueAndRegisterAreThreadSafe(t *testing.T) {
	r := New()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			handler := func(ctx context.Context, p int) error { return nil }
			Register[int](r, "", handler)
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Enqueue[int](context.Background(), r, "", 0)
		}()
	}
	wg.Wait()
}

func BenchmarkEnqueue(b *testing.B) {
	r := New()
	Register[int](r, "bench", func(ctx context.Context, p int) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Enqueue[int](context.Background(), r, "bench", i)
	}
}
