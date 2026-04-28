package broker

import (
	"context"
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