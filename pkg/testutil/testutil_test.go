package testutil

import (
	"strings"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

type testService struct {
	Name string
}

func TestTestHostHTTPAndResolve(t *testing.T) {
	host := NewTestHost(func(b *di.Builder, app *srv.Server) {
		di.RegisterInstance[*testService](b, &testService{Name: "svc"})
		app.MapGet("/ping", func(ctx *srv.Context) error {
			return ctx.JSON(201, map[string]string{"ok": "yes"})
		})
		app.MapPost("/echo", func(ctx *srv.Context) error {
			var body map[string]string
			if err := ctx.Bind(&body); err != nil {
				return err
			}
			return ctx.JSON(200, body)
		})
	})
	defer host.Close()

	service := Resolve[*testService](host)
	if service.Name != "svc" {
		t.Fatalf("Resolve() name = %q, want svc", service.Name)
	}

	resp := host.Get("/ping")
	if resp.Error != nil {
		t.Fatalf("Get() error = %v", resp.Error)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("Get() status = %d, want 201", resp.StatusCode)
	}

	var decoded map[string]string
	if err := resp.Decode(&decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded["ok"] != "yes" {
		t.Fatalf("Decode()[ok] = %q, want yes", decoded["ok"])
	}

	post := host.Post("/echo", "application/json", strings.NewReader(`{"msg":"hello"}`))
	if post.StatusCode != 200 {
		t.Fatalf("Post() status = %d, want 200", post.StatusCode)
	}
}

func TestTestHostCleanup(t *testing.T) {
	host := NewTestHost(func(b *di.Builder, app *srv.Server) {})
	called := false
	host.Cleanup(func() { called = true })

	host.Close()

	if !called {
		t.Fatal("cleanup was not called")
	}
}

func TestFakeLoggerAssertLogged(t *testing.T) {
	logger := NewFakeLogger()
	logger.Entries = append(logger.Entries, "ready")

	logger.AssertLogged(t, "ready")
}

func TestTestContext(t *testing.T) {
	ctx := NewTestContext()
	ctx.Vars["key"] = "value"

	if ctx.Ctx == nil {
		t.Fatal("Ctx is nil")
	}
	if ctx.Vars["key"] != "value" {
		t.Fatalf("Vars[key] = %v, want value", ctx.Vars["key"])
	}
}
