package pipeline

import (
	"context"
	"errors"
	"testing"
)

func TestPipelineProcessOrder(t *testing.T) {
	var calls []string
	p := New[string, string]().
		Use(func(ctx context.Context, input string, next func(context.Context, string) (string, error)) (string, error) {
			calls = append(calls, "before-a")
			out, err := next(ctx, input+"a")
			calls = append(calls, "after-a")
			return out + "A", err
		}).
		Use(func(ctx context.Context, input string, next func(context.Context, string) (string, error)) (string, error) {
			calls = append(calls, "before-b")
			out, err := next(ctx, input+"b")
			calls = append(calls, "after-b")
			return out + "B", err
		}).
		Then(func(ctx context.Context, input string) (string, error) {
			calls = append(calls, "handler")
			return input + "h", nil
		})

	got, err := p.Process(context.Background(), "")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got != "abhBA" {
		t.Fatalf("Process() = %q, want abhBA", got)
	}

	want := []string{"before-a", "before-b", "handler", "after-b", "after-a"}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestPipelineProcessWithoutHandler(t *testing.T) {
	got, err := New[string, int]().Process(context.Background(), "input")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("Process() = %d, want zero", got)
	}
}

func TestPipelineReturnsError(t *testing.T) {
	want := errors.New("stop")
	p := New[string, string]().
		Then(func(ctx context.Context, input string) (string, error) {
			return "", want
		})

	_, err := p.Process(context.Background(), "")
	if !errors.Is(err, want) {
		t.Fatalf("Process() error = %v, want %v", err, want)
	}
}
