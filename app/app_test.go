//foundation:ignore-file

package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/app/actions"
	"github.com/mirkobrombin/go-foundation/v2/app/web"
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

func TestApp_LogConfiguresScheduler(t *testing.T) {
	var output bytes.Buffer
	application := New().Log(slog.New(slog.NewTextHandler(&output, nil)))
	if err := application.sched.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := application.sched.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "scheduler: started") {
		t.Fatalf("scheduler log was not routed through App.Log(): %s", output.String())
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

func TestApp_BuildIsIdempotent(t *testing.T) {
	application := New()
	first, err := application.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	second, err := application.Build()
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if first != second {
		t.Fatal("Build() returned a different container")
	}
}

func TestApp_RegistersStaticDefinitionsAfterBuild(t *testing.T) {
	application := New()
	if _, err := application.Build(); err != nil {
		t.Fatal(err)
	}
	application.RegisterActionDefinition(actions.Definition{
		Name: "late.action",
		New:  func() actions.Handler { return &saveAction{} },
	})

	result, err := application.Dispatch(context.Background(), "late.action", map[string]any{"name": "late.md"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "saved late.md" {
		t.Fatalf("Dispatch() = %v", result)
	}
}

type actionWithDependency struct {
	Value string `inject:"value"`
}

func (a *actionWithDependency) Handle(context.Context) (any, error) {
	return a.Value, nil
}

func TestApp_BuildValidatesActionDependencies(t *testing.T) {
	application := New()
	application.RegisterActionDefinition(actions.Definition{
		Name: "requires.value",
		New:  func() actions.Handler { return &actionWithDependency{} },
	})

	if _, err := application.Build(); err == nil {
		t.Fatal("Build() accepted an action with a missing dependency")
	}
}

type endpointWithDependency struct {
	Meta  struct{} `method:"GET" path:"/requires-value"`
	Value string   `inject:"value"`
}

func (e *endpointWithDependency) Handle(context.Context) (any, error) {
	return e.Value, nil
}

func TestApp_BuildCanRetryWithoutPartialRouteRegistration(t *testing.T) {
	application := New()
	application.RegisterHTTP(&endpointWithDependency{})

	if _, err := application.Build(); err == nil {
		t.Fatal("first Build() accepted a missing HTTP dependency")
	}
	before := httptest.NewRecorder()
	application.server.ServeHTTP(
		before,
		httptest.NewRequest(http.MethodGet, "/requires-value", nil),
	)
	if before.Code != http.StatusNotFound {
		t.Fatalf("failed Build() registered a route with status %d", before.Code)
	}

	application.Provide("value", "ready")
	if _, err := application.Build(); err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	after := httptest.NewRecorder()
	application.server.ServeHTTP(
		after,
		httptest.NewRequest(http.MethodGet, "/requires-value", nil),
	)
	if after.Code != http.StatusOK {
		t.Fatalf("second Build() route status = %d, body = %s", after.Code, after.Body.String())
	}
}

type appCloser struct {
	closed bool
}

func (c *appCloser) Close() error {
	c.closed = true
	return nil
}

func TestApp_FailedBuildDoesNotCloseProvidedDependencies(t *testing.T) {
	closer := &appCloser{}
	application := New().
		Provide("db", closer).
		RegisterActionDefinition(actions.Definition{
			Name: "requires.value",
			New:  func() actions.Handler { return &actionWithDependency{} },
		})

	if _, err := application.Build(); err == nil {
		t.Fatal("first Build() accepted a missing action dependency")
	}
	if closer.closed {
		t.Fatal("failed Build() closed a dependency owned by the reusable builder")
	}

	application.Provide("value", "ready")
	container, err := application.Build()
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if closer.closed {
		t.Fatal("successful retry received an already-closed dependency")
	}
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if !closer.closed {
		t.Fatal("container Close() did not close the provided dependency")
	}
}

func TestApp_PostBuildHTTPDefinitionsAreTransactional(t *testing.T) {
	application := New()
	if _, err := application.Build(); err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("RegisterHTTPDefinitions() accepted an invalid batch")
			}
		}()
		application.RegisterHTTPDefinitions(
			web.HandlerDefinition{
				Method: http.MethodGet,
				Path:   "/batch-first",
				New:    func() web.Handler { return &greetEndpoint{} },
			},
			web.HandlerDefinition{
				Method: http.MethodGet,
				Path:   "",
				New:    func() web.Handler { return &greetEndpoint{} },
			},
		)
	}()

	for _, route := range application.server.Routes() {
		if route.Path == "/batch-first" {
			t.Fatal("invalid batch partially registered its first route")
		}
	}
}

func TestApp_BuildRejectsCrossRouterActionDuplicate(t *testing.T) {
	application := New()
	application.RegisterAction("file.save", func(context.Context, ...any) (any, error) {
		return nil, nil
	})
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterActionDefinition() accepted a cross-router duplicate")
		}
	}()
	application.RegisterActionDefinition(actions.Definition{
		Name: "file.save",
		New:  func() actions.Handler { return &saveAction{} },
	})
}

func TestApp_RejectsCrossRouterActionDuplicateAfterBuild(t *testing.T) {
	t.Run("dispatcher first", func(t *testing.T) {
		application := New()
		if _, err := application.Build(); err != nil {
			t.Fatal(err)
		}
		application.RegisterAction("duplicate", func(context.Context, ...any) (any, error) {
			return nil, nil
		})
		defer func() {
			if recover() == nil {
				t.Fatal("RegisterActionDefinition() accepted a dispatcher duplicate")
			}
		}()
		application.RegisterActionDefinition(actions.Definition{
			Name: "duplicate",
			New:  func() actions.Handler { return &saveAction{} },
		})
	})

	t.Run("declarative first", func(t *testing.T) {
		application := New()
		if _, err := application.Build(); err != nil {
			t.Fatal(err)
		}
		application.RegisterActionDefinition(actions.Definition{
			Name: "duplicate",
			New:  func() actions.Handler { return &saveAction{} },
		})
		defer func() {
			if recover() == nil {
				t.Fatal("RegisterAction() accepted a declarative duplicate")
			}
		}()
		application.RegisterAction("duplicate", func(context.Context, ...any) (any, error) {
			return nil, nil
		})
	})

	t.Run("reflection second", func(t *testing.T) {
		application := New()
		if _, err := application.Build(); err != nil {
			t.Fatal(err)
		}
		application.RegisterAction("file.save", func(context.Context, ...any) (any, error) {
			return nil, nil
		})
		defer func() {
			if recover() == nil {
				t.Fatal("RegisterActionHandler() accepted a dispatcher duplicate")
			}
		}()
		application.RegisterActionHandler(&saveAction{})
	})
}
