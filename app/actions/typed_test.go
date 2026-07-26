package actions

import (
	"context"
	"testing"
)

type renameInput struct {
	Name string
}

type renameResult struct {
	Path string
}

func TestTypedAction(t *testing.T) {
	action := NewTyped[renameInput, renameResult]("file.rename")
	router := New()
	err := HandleTyped(router, action, func(_ context.Context, input renameInput) (renameResult, error) {
		return renameResult{Path: input.Name}, nil
	})
	if err != nil {
		t.Fatalf("HandleTyped() error = %v", err)
	}

	got, err := DispatchTyped(context.Background(), router, action, renameInput{Name: "new.md"})
	if err != nil {
		t.Fatalf("DispatchTyped() error = %v", err)
	}
	if got.Path != "new.md" {
		t.Fatalf("DispatchTyped() = %#v", got)
	}
	if !router.Has(action.Name()) {
		t.Fatal("Has() did not report a typed action")
	}
}

func TestTypedActionRejectsRuntimeTypeMismatch(t *testing.T) {
	action := NewTyped[renameInput, renameResult]("file.rename")
	router := New()
	if err := HandleTyped(router, action, func(_ context.Context, input renameInput) (renameResult, error) {
		return renameResult{Path: input.Name}, nil
	}); err != nil {
		t.Fatalf("HandleTyped() error = %v", err)
	}

	if _, err := router.Dispatch(context.Background(), action.Name(), "wrong"); err == nil {
		t.Fatal("Dispatch() accepted the wrong payload type")
	}
}
