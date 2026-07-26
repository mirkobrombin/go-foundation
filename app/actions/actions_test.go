//foundation:ignore-file

package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/core/events"
)

type saveAction struct {
	Meta     struct{}  `action:"file.save" keys:"ctrl+s"`
	Document *document `inject:"document"`
	Name     string    `json:"name"`
	Count    int       `json:"count"`
	IsAdmin  bool
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

func TestRouter_AsyncEventsPropagateClosedBus(t *testing.T) {
	bus := events.New()
	if err := bus.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	r := New(WithAsyncEvents(bus))
	r.Provide("document", &document{})
	r.Register(&saveAction{})

	_, err := r.Dispatch(context.Background(), "file.save", map[string]any{"name": "event.md"})
	if !errors.Is(err, events.ErrBusClosed) {
		t.Fatalf("Dispatch() error = %v, want ErrBusClosed", err)
	}
}

func TestRouter_MissingAction(t *testing.T) {
	r := New()

	if _, err := r.Dispatch(context.Background(), "missing"); err == nil {
		t.Fatal("Dispatch() error = nil, want error")
	}
}

func TestRouter_RejectsUnknownPayloadField(t *testing.T) {
	r := New()
	r.Provide("document", &document{})
	r.Register(&saveAction{})

	if _, err := r.Dispatch(context.Background(), "file.save", map[string]any{"typo": "notes.md"}); err == nil {
		t.Fatal("Dispatch() accepted an unknown payload field")
	}
	if _, err := r.Dispatch(context.Background(), "file.save", map[string]any{"typo": nil}); err == nil {
		t.Fatal("Dispatch() accepted an unknown nil payload field")
	}
}

func TestRouter_PayloadCannotOverwriteInjectedDependency(t *testing.T) {
	trusted := &document{}
	attacker := &document{}
	r := New()
	r.Provide("document", trusted)
	r.Register(&saveAction{})

	if _, err := r.Dispatch(context.Background(), "file.save", map[string]any{
		"document": attacker,
		"name":     "trusted.md",
	}); err == nil {
		t.Fatal("Dispatch() accepted an injected field in the payload")
	}
	if attacker.saved != "" {
		t.Fatalf("payload replaced injected dependency: %#v", attacker)
	}
}

func TestRouter_PayloadRequiresExplicitFieldTag(t *testing.T) {
	r := New()
	r.Provide("document", &document{})
	r.Register(&saveAction{})

	if _, err := r.Dispatch(context.Background(), "file.save", map[string]any{
		"isAdmin": true,
	}); err == nil {
		t.Fatal("Dispatch() accepted an untagged exported field")
	}
}

type missingDependencyAction struct {
	_        struct{}  `action:"missing.dependency"`
	Document *document `inject:"missing"`
}

func (a *missingDependencyAction) Handle(_ context.Context) (any, error) {
	return a.Document, nil
}

func TestRouter_RejectsMissingDependency(t *testing.T) {
	r := New()
	r.Register(&missingDependencyAction{})

	if _, err := r.Dispatch(context.Background(), "missing.dependency"); err == nil {
		t.Fatal("Dispatch() accepted a missing dependency")
	}
}

func TestRouter_RegisterDefinition(t *testing.T) {
	r := New()
	doc := &document{}
	r.Provide("document", doc)
	err := r.RegisterDefinition(Definition{
		Name: "static.save",
		Key:  "ctrl+shift+s",
		New: func() Handler {
			return &saveAction{}
		},
	})
	if err != nil {
		t.Fatalf("RegisterDefinition() error = %v", err)
	}

	result, err := r.Dispatch(context.Background(), "static.save", map[string]any{"name": "static.md"})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result != 0 || doc.saved != "static.md" {
		t.Fatalf("Dispatch() = %v, saved = %q", result, doc.saved)
	}
}

func TestRouter_RegisterDefinitionsIsTransactional(t *testing.T) {
	router := New()
	err := router.RegisterDefinitions(
		Definition{Name: "one", Key: "ctrl+x", New: func() Handler { return &saveAction{} }},
		Definition{Name: "two", Key: "ctrl+x", New: func() Handler { return &duplicateKeyAction{} }},
	)
	if err == nil {
		t.Fatal("RegisterDefinitions() accepted a duplicate key")
	}
	if len(router.Actions()) != 0 {
		t.Fatalf("RegisterDefinitions() left actions after failure: %v", router.Actions())
	}
}

func TestRouter_RegisterDefinitionValidatesInjection(t *testing.T) {
	router := New(WithContainer(di.NewBuilder().MustBuild()))
	err := router.RegisterDefinition(Definition{
		Name: "missing.dependency",
		New:  func() Handler { return &missingDependencyAction{} },
	})
	if err == nil {
		t.Fatal("RegisterDefinition() accepted a missing dependency")
	}
}
