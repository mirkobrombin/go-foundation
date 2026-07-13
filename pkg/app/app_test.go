package app

import (
	"context"
	"testing"
)

type greetEndpoint struct {
	Meta struct{} `method:"GET" path:"/greet"`
	Name string   `query:"name" default:"world"`
}

type greetResponse struct {
	Message string `json:"message"`
}

func (e *greetEndpoint) Handle(_ context.Context) (any, error) {
	return greetResponse{Message: "hello " + e.Name}, nil
}

func TestApp_New(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
}

func TestApp_Provide(t *testing.T) {
	a := New()
	a.Provide("db", "fake-connection")
}

func TestApp_RegisterHTTP(t *testing.T) {
	a := New()
	a.RegisterHTTP(&greetEndpoint{})
}

func TestApp_RegisterAction(t *testing.T) {
	a := New()
	a.RegisterAction("test", func(ctx context.Context, payload ...any) (any, error) {
		return "ok", nil
	})
}

func TestApp_Dispatch(t *testing.T) {
	a := New()
	a.RegisterAction("ping", func(ctx context.Context, payload ...any) (any, error) {
		return "pong", nil
	})
	result, err := a.Dispatch(context.Background(), "ping")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result != "pong" {
		t.Errorf("result = %v, want pong", result)
	}
}

type saveAction struct {
	Meta struct{} `action:"file.save" keys:"ctrl+s"`
	Name string   `json:"name"`
}

func (a *saveAction) Handle(_ context.Context) (any, error) {
	return "saved " + a.Name, nil
}

func TestApp_RegisterActionHandler(t *testing.T) {
	a := New()
	a.RegisterActionHandler(&saveAction{})

	result, err := a.Dispatch(context.Background(), "file.save", map[string]any{"name": "notes.md"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result != "saved notes.md" {
		t.Errorf("result = %v, want saved notes.md", result)
	}
}

func TestApp_DispatchKey(t *testing.T) {
	a := New()
	a.RegisterActionHandler(&saveAction{})

	result, err := a.DispatchKey(context.Background(), "ctrl+s", map[string]any{"name": "book.md"})
	if err != nil {
		t.Fatalf("DispatchKey: %v", err)
	}
	if result != "saved book.md" {
		t.Errorf("result = %v, want saved book.md", result)
	}
}

func TestApp_Schedule(t *testing.T) {
	a := New()
	a.Schedule("cleanup", "0 0 * * *", func(ctx context.Context) error {
		return nil
	})
}
