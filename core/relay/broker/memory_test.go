package broker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryBroker_PublishSubscribe(t *testing.T) {
	b := NewMemoryBroker()
	ch := make(chan []byte, 1)

	b.Subscribe("test", func(_ context.Context, data []byte) error {
		ch <- data
		return nil
	})

	err := b.Publish(context.Background(), "test", []byte("hello"))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case result := <-ch:
		if string(result) != "hello" {
			t.Errorf("got %q, want %q", string(result), "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler")
	}
}

func TestMemoryBroker_MultipleSubscribers(t *testing.T) {
	b := NewMemoryBroker()
	ch := make(chan int, 3)
	count := 0

	for i := 0; i < 3; i++ {
		b.Subscribe("topic", func(_ context.Context, data []byte) error {
			ch <- 1
			return nil
		})
	}

	b.Publish(context.Background(), "topic", []byte("data"))

	for i := 0; i < 3; i++ {
		select {
		case <-ch:
			count++
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for handler %d", i)
		}
	}
	if count != 3 {
		t.Errorf("expected 3 handler calls, got %d", count)
	}
}

func TestMemoryBroker_DifferentTopics(t *testing.T) {
	b := NewMemoryBroker()
	ch := make(chan string, 1)

	b.Subscribe("topic1", func(_ context.Context, data []byte) error {
		ch <- "topic1:" + string(data)
		return nil
	})
	b.Subscribe("topic2", func(_ context.Context, data []byte) error {
		ch <- "topic2:" + string(data)
		return nil
	})

	b.Publish(context.Background(), "topic1", []byte("msg"))

	select {
	case result := <-ch:
		if result != "topic1:msg" {
			t.Errorf("got %q, want %q", result, "topic1:msg")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler")
	}
}

func TestMemoryBroker_PublishNoSubscribers(t *testing.T) {
	b := NewMemoryBroker()
	err := b.Publish(context.Background(), "nonexistent", []byte("msg"))
	if err != nil {
		t.Fatalf("Publish to nonexistent topic: %v", err)
	}
}

func TestMemoryBroker_PropagatesSubscriberError(t *testing.T) {
	broker := NewMemoryBroker()
	want := errors.New("delivery failed")
	var successfulCalls int
	if _, err := broker.Subscribe("topic", func(context.Context, []byte) error {
		return want
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Subscribe("topic", func(context.Context, []byte) error {
		successfulCalls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(context.Background(), "topic", nil); !errors.Is(err, want) {
		t.Fatalf("Publish() error = %v, want %v", err, want)
	}
	if successfulCalls != 1 {
		t.Fatalf("successful subscriber calls = %d, want 1", successfulCalls)
	}
}

func TestMemoryBrokerCopiesPayloadPerSubscriber(t *testing.T) {
	broker := NewMemoryBroker()
	if _, err := broker.Subscribe("topic", func(_ context.Context, payload []byte) error {
		payload[0] = 'X'
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var received string
	if _, err := broker.Subscribe("topic", func(_ context.Context, payload []byte) error {
		received = string(payload)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(context.Background(), "topic", []byte("safe")); err != nil {
		t.Fatal(err)
	}
	if received != "safe" {
		t.Fatalf("second subscriber received %q", received)
	}
}

func TestMemoryBrokerUnsubscribe(t *testing.T) {
	broker := NewMemoryBroker()
	calls := 0
	subscription, err := broker.Subscribe("topic", func(context.Context, []byte) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(context.Background(), "topic", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("handler called %d times after unsubscribe", calls)
	}
}

func TestMemoryBroker_ConcurrentPublish(t *testing.T) {
	b := NewMemoryBroker()
	ch := make(chan int, 10)
	received := 0

	b.Subscribe("test", func(_ context.Context, data []byte) error {
		ch <- 1
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(context.Background(), "test", []byte("data"))
		}()
	}
	wg.Wait()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				received++
				if received >= 10 {
					close(done)
					return
				}
			case <-time.After(100 * time.Millisecond):
				close(done)
				return
			}
		}
	}()

	select {
	case <-done:
		if received != 10 {
			t.Errorf("expected 10 handler calls, got %d", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
