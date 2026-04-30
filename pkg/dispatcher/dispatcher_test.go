package dispatcher

import (
	"context"
	"testing"
)

func TestDispatcher_RegisterAndDispatch(t *testing.T) {
	d := New()
	d.Register("greet", func(ctx context.Context, payload ...any) (any, error) {
		name := "world"
		if len(payload) > 0 {
			if s, ok := payload[0].(string); ok {
				name = s
			}
		}
		return "hello " + name, nil
	})

	result, err := d.Dispatch(context.Background(), "greet", "alice")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result != "hello alice" {
		t.Errorf("result = %v, want hello alice", result)
	}
}

func TestDispatcher_NotFound(t *testing.T) {
	d := New()
	_, err := d.Dispatch(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing handler")
	}
}

func TestDispatcher_Has(t *testing.T) {
	d := New()
	if d.Has("action") {
		t.Error("Has should be false before register")
	}
	d.Register("action", func(ctx context.Context, payload ...any) (any, error) {
		return nil, nil
	})
	if !d.Has("action") {
		t.Error("Has should be true after register")
	}
}

func TestDispatcher_Names(t *testing.T) {
	d := New()
	d.Register("a", func(ctx context.Context, payload ...any) (any, error) { return nil, nil })
	d.Register("b", func(ctx context.Context, payload ...any) (any, error) { return nil, nil })

	names := d.Names()
	if len(names) != 2 {
		t.Errorf("Names length = %d, want 2", len(names))
	}
}

func TestDispatcher_DuplicatePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate register")
		}
	}()
	d := New()
	d.Register("dup", func(ctx context.Context, payload ...any) (any, error) { return nil, nil })
	d.Register("dup", func(ctx context.Context, payload ...any) (any, error) { return nil, nil })
}