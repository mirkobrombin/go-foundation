package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type testOrder struct {
	Status string
}

func (o *testOrder) OnEnterPaid() {
	o.Status = "paid"
}

func (o *testOrder) OnEnterShipped() {
	o.Status = "shipped"
}

func (o *testOrder) OnExitDraft() {
	// cleanup
}

func (o *testOrder) CanEnterPaid() bool {
	return o.Status == "draft"
}

func (o *testOrder) Before() error {
	return nil
}

func TestDiscovery_Discover(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{Status: "draft"}

	methods := d.Discover(order, "OnEnter")

	if len(methods) != 2 {
		t.Fatalf("got %d methods, want 2", len(methods))
	}

	names := make(map[string]bool)
	for _, m := range methods {
		names[m.Name] = true
		if m.Suffix != "Paid" && m.Suffix != "Shipped" {
			t.Errorf("unexpected suffix: %q", m.Suffix)
		}
	}

	if !names["OnEnterPaid"] || !names["OnEnterShipped"] {
		t.Error("missing expected methods")
	}
}

func TestDiscovery_DiscoverAll(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{}

	all := d.DiscoverAll(order, "OnEnter", "OnExit", "Can")

	if len(all["OnEnter"]) != 2 {
		t.Errorf("OnEnter: got %d, want 2", len(all["OnEnter"]))
	}

	if len(all["OnExit"]) != 1 {
		t.Errorf("OnExit: got %d, want 1", len(all["OnExit"]))
	}

	if len(all["Can"]) != 1 {
		t.Errorf("Can: got %d, want 1", len(all["Can"]))
	}
}

func TestDiscovery_HasMethod(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{}

	if !d.HasMethod(order, "OnEnterPaid") {
		t.Error("should have OnEnterPaid")
	}

	if d.HasMethod(order, "OnEnterCancelled") {
		t.Error("should not have OnEnterCancelled")
	}
}

func TestDiscovery_Call(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{Status: "draft"}

	_, err := d.Call(order, "OnEnterPaid")
	if err != nil {
		t.Fatal(err)
	}

	if order.Status != "paid" {
		t.Errorf("status: got %q, want %q", order.Status, "paid")
	}
}

func TestDiscovery_Call_Missing(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{}

	result, err := d.Call(order, "MissingMethod")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("call to missing method should return nil")
	}
}

func TestDiscovery_Caching(t *testing.T) {
	d := NewDiscovery()
	order := &testOrder{}

	methods1 := d.Discover(order, "OnEnter")
	methods2 := d.Discover(order, "OnEnter")

	if len(methods1) != len(methods2) {
		t.Error("cached result should match")
	}
}

func TestRunner_BeforeAfter(t *testing.T) {
	r := NewRunner()

	var order []string

	r.Before("save", func(ctx context.Context, key string, args []any) error {
		order = append(order, "before")
		return nil
	})

	r.After("save", func(ctx context.Context, key string, args []any) error {
		order = append(order, "after")
		return nil
	})

	err := r.Run(context.Background(), "save", func() error {
		order = append(order, "action")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(order) != 3 || order[0] != "before" || order[1] != "action" || order[2] != "after" {
		t.Errorf("wrong order: %v", order)
	}
}

func TestRunner_BeforeAll(t *testing.T) {
	r := NewRunner()

	calls := 0
	r.BeforeAll(func(ctx context.Context, key string, args []any) error {
		calls++
		return nil
	})

	r.Run(context.Background(), "event1", func() error { return nil })
	r.Run(context.Background(), "event2", func() error { return nil })

	if calls != 2 {
		t.Errorf("BeforeAll should be called twice, got %d", calls)
	}
}

func TestRunner_ErrorStopsExecution(t *testing.T) {
	r := NewRunner()

	r.Before("fail", func(ctx context.Context, key string, args []any) error {
		return errors.New("before error")
	})

	actionCalled := false
	err := r.Run(context.Background(), "fail", func() error {
		actionCalled = true
		return nil
	})

	if err == nil {
		t.Error("expected error from Before hook")
	}

	if actionCalled {
		t.Error("action should not be called when Before hook fails")
	}
}

func TestRunner_Clear(t *testing.T) {
	r := NewRunner()

	r.Before("test", func(ctx context.Context, key string, args []any) error {
		return errors.New("should not run")
	})

	r.Clear()

	err := r.Run(context.Background(), "test", func() error { return nil })
	if err != nil {
		t.Error("hooks should be cleared")
	}
}

func TestRunnerParallelRecoversHookPanic(t *testing.T) {
	runner := NewRunner()
	runner.Before("panic", func(context.Context, string, []any) error {
		panic("hook failed")
	})

	done := make(chan error, 1)
	go func() {
		done <- runner.RunParallel(context.Background(), "panic")
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "hook failed") {
			t.Fatalf("RunParallel() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunParallel() did not return after a hook panic")
	}
}
