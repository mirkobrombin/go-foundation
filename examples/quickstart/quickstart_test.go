package quickstart

import (
	"context"
	"testing"
)

func TestBuildAndDispatchGeneratedAction(t *testing.T) {
	application, err := Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	result, err := application.Dispatch(
		context.Background(),
		"users.create",
		map[string]any{"Name": "Ada"},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	user, ok := result.(User)
	if !ok {
		t.Fatalf("Dispatch() result type = %T", result)
	}
	if user.ID != 1 || user.Name != "Ada" {
		t.Fatalf("Dispatch() result = %#v", user)
	}
}

func TestGeneratedDefinitions(t *testing.T) {
	handlers := FoundationHTTPHandlers()
	if len(handlers) != 1 || handlers[0].Method != "GET" || handlers[0].Path != "/users/{id:int}" {
		t.Fatalf("FoundationHTTPHandlers() = %#v", handlers)
	}

	actions := FoundationActions()
	if len(actions) != 1 || actions[0].Name != "users.create" || actions[0].Key != "ctrl+n" {
		t.Fatalf("FoundationActions() = %#v", actions)
	}
}
