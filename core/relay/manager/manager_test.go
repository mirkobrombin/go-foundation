package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/relay/broker"
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

func TestRegisterRejectsDuplicateTopic(t *testing.T) {
	relay := New()
	if err := Register[int](relay, "topic", func(context.Context, int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Register[int](relay, "topic", func(context.Context, int) error { return nil }); err == nil {
		t.Fatal("Register() accepted a duplicate topic")
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
	if err == nil {
		t.Fatal("Start accepted a cancelled context")
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

func TestStartRejectsDuplicateAndContextCancellationUnsubscribes(t *testing.T) {
	r := New()
	var calls int
	if err := Register[int](r, "topic", func(context.Context, int) error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready, err := r.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	if _, err := r.Start(context.Background()); err == nil {
		t.Fatal("second Start() succeeded")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		r.lifecycle.Lock()
		started := r.started
		r.lifecycle.Unlock()
		if !started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay did not stop after context cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	if err := Enqueue(context.Background(), r, "topic", 1); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("handler called %d times after stop", calls)
	}
}

func TestRegisterRejectsOversizedMessage(t *testing.T) {
	r := New()
	if err := Register[string](r, "topic", func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(context.Background(), r, "topic", string(make([]byte, maxMessageSize))); err == nil {
		t.Fatal("Enqueue() accepted an oversized message")
	}
}

type reentrantSubscription struct {
	relay *Relay
}

func (s *reentrantSubscription) Unsubscribe() error {
	_, err := s.relay.Start(context.Background())
	if err == nil {
		return errors.New("relay restarted during unsubscribe")
	}
	return nil
}

type reentrantBroker struct {
	relay *Relay
}

func (b *reentrantBroker) Publish(context.Context, string, []byte) error {
	return nil
}

func (b *reentrantBroker) Subscribe(string, broker.Handler) (broker.Subscription, error) {
	if err := b.relay.Stop(); err != nil {
		return nil, err
	}
	return &reentrantSubscription{relay: b.relay}, nil
}

func TestRelayBrokerCallbacksCanReenterLifecycle(t *testing.T) {
	customBroker := &reentrantBroker{}
	relay := New(WithBroker(customBroker))
	customBroker.relay = relay
	if err := Register[int](relay, "topic", func(context.Context, int) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := relay.Start(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start() ignored the reentrant Stop()")
		}
	case <-time.After(time.Second):
		t.Fatal("Start() deadlocked in a reentrant broker callback")
	}
}
