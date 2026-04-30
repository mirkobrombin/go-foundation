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

func TestApp_Schedule(t *testing.T) {
	a := New()
	a.Schedule("cleanup", "0 0 * * *", func(ctx context.Context) error {
		return nil
	})
}