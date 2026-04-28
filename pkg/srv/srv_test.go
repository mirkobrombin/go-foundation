package srv

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer_MapGet(t *testing.T) {
	s := New()
	s.MapGet("/hello", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"message": "hello"})
	})

	req := httptest.NewRequest("GET", "/hello", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] != "hello" {
		t.Errorf("body: got %q, want %q", resp["message"], "hello")
	}
}

func TestServer_Params(t *testing.T) {
	s := New()
	s.MapGet("/users/{id}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"id": ctx.Params["id"]})
	})

	req := httptest.NewRequest("GET", "/users/42", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] != "42" {
		t.Errorf("param: got %q, want %q", resp["id"], "42")
	}
}

func TestServer_NotFound(t *testing.T) {
	s := New()
	req := httptest.NewRequest("GET", "/missing", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

func TestServer_Middleware(t *testing.T) {
	s := New()
	s.Use(RequestID())

	s.MapGet("/test", func(ctx *Context) error {
		id, _ := ctx.Get("request_id")
		return ctx.JSON(200, map[string]any{"request_id": id})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "test-123")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["request_id"] != "test-123" {
		t.Errorf("middleware: got %v, want test-123", resp["request_id"])
	}
}

func TestServer_Group(t *testing.T) {
	s := New()
	api := s.Group("/api")
	api.MapGet("/users", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"path": "/api/users"})
	})

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("group route: got %d, want 200", w.Code)
	}
}

func TestServer_CORS(t *testing.T) {
	s := New()
	s.Use(CORS("*"))
	s.MapGet("/test", func(ctx *Context) error {
		ctx.String(200, "ok")
		return nil
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("CORS preflight: got %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS header: got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestServer_Bind(t *testing.T) {
	s := New()
	s.MapPost("/data", func(ctx *Context) error {
		var body map[string]string
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		return ctx.JSON(200, body)
	})

	req := httptest.NewRequest("POST", "/data", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("bind: got %d, want 200", w.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := New()
	s.MapGet("/health", HealthEndpoint(func(ctx context.Context) error {
		return nil
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("health: got %d, want 200", w.Code)
	}
}