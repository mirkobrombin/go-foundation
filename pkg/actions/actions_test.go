package actions

import (
	"context"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/events"
)

type saveAction struct {
	Meta     struct{}  `action:"file.save" keys:"ctrl+s"`
	Document *document `inject:"document"`
	Name     string    `json:"name"`
	Count    int       `json:"count"`
}

type document struct {
	saved string
}

func (a *saveAction) Handle(_ context.Context) (any, error) {
	a.Document.saved = a.Name
	return a.Count, nil
}

type duplicateKeyAction struct {
	Meta struct{} `action:"file.save-as" keys:"ctrl+s"`
}

func (a *duplicateKeyAction) Handle(_ context.Context) (any, error) {
	return nil, nil
}

func TestRouter_Dispatch(t *testing.T) {
	doc := &document{}
	r := New()
	r.Provide("document", doc)
	r.Register(&saveAction{})

	result, err := r.Dispatch(context.Background(), "file.save", map[string]any{
		"name":  "notes.md",
		"count": 2,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result != 2 {
		t.Fatalf("result = %v, want 2", result)
	}
	if doc.saved != "notes.md" {
		t.Fatalf("saved = %q, want notes.md", doc.saved)
	}
}

func TestRouter_DispatchKey(t *testing.T) {
	doc := &document{}
	r := New()
	r.Provide("document", doc)
	r.Register(&saveAction{})

	_, err := r.DispatchKey(context.Background(), "ctrl+s", struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{Name: "book.md", Count: 1})
	if err != nil {
		t.Fatalf("DispatchKey() error = %v", err)
	}
	if doc.saved != "book.md" {
		t.Fatalf("saved = %q, want book.md", doc.saved)
	}
}

func TestRouter_ActionsAndKeyBindings(t *testing.T) {
	r := New()
	r.Register(&saveAction{})

	actions := r.Actions()
	if len(actions) != 1 || actions[0] != "file.save" {
		t.Fatalf("Actions() = %v", actions)
	}

	bindings := r.KeyBindings()
	if bindings["ctrl+s"] != "file.save" {
		t.Fatalf("KeyBindings() = %v", bindings)
	}
}

func TestRouter_DuplicateKeyBindingPanics(t *testing.T) {
	r := New()
	r.Register(&saveAction{})

	defer func() {
		if recover() == nil {
			t.Fatal("Register() did not panic")
		}
	}()

	r.Register(&duplicateKeyAction{})
}

func TestRouter_Events(t *testing.T) {
	bus := events.New()
	defer bus.Close()

	var seen *saveAction
	events.Subscribe(bus, func(ctx context.Context, e *saveAction) error {
		seen = e
		return nil
	})

	r := New(WithEvents(bus))
	r.Provide("document", &document{})
	r.Register(&saveAction{})

	_, err := r.Dispatch(context.Background(), "file.save", map[string]any{"name": "event.md"})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if seen == nil || seen.Name != "event.md" {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestRouter_MissingAction(t *testing.T) {
	r := New()

	if _, err := r.Dispatch(context.Background(), "missing"); err == nil {
		t.Fatal("Dispatch() error = nil, want error")
	}
}
