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

// --- M3: Constraint, catch-all, method multiplex tests ---

func TestServer_IntConstraint(t *testing.T) {
	s := New()
	s.MapGet("/users/{id:int}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"id": ctx.Params["id"]})
	})

	req := httptest.NewRequest("GET", "/users/42", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("int constraint valid: got %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/users/abc", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("int constraint invalid: got %d, want 404", w.Code)
	}
}

func TestServer_AlphaConstraint(t *testing.T) {
	s := New()
	s.MapGet("/items/{slug:alpha}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"slug": ctx.Params["slug"]})
	})

	req := httptest.NewRequest("GET", "/items/hello", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("alpha constraint valid: got %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/items/hello123", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("alpha constraint invalid: got %d, want 404", w.Code)
	}
}

func TestServer_CatchAll(t *testing.T) {
	s := New()
	s.MapGet("/static/{*filepath}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"path": ctx.Params["filepath"]})
	})

	req := httptest.NewRequest("GET", "/static/css/main.css", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("catch-all: got %d, want 200", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["path"] != "css/main.css" {
		t.Errorf("catch-all path: got %q, want %q", resp["path"], "css/main.css")
	}

	req = httptest.NewRequest("GET", "/static/a/b/c/d", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("deep catch-all: got %d, want 200", w.Code)
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["path"] != "a/b/c/d" {
		t.Errorf("deep catch-all: got %q, want %q", resp["path"], "a/b/c/d")
	}
}

func TestServer_MethodMultiplex(t *testing.T) {
	s := New()
	s.MapGet("/items/{id}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"method": "GET"})
	})
	s.MapPost("/items/{id}", func(ctx *Context) error {
		return ctx.JSON(201, map[string]string{"method": "POST"})
	})
	s.MapDelete("/items/{id}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"method": "DELETE"})
	})

	req := httptest.NewRequest("GET", "/items/1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("GET: got %d, want 200", w.Code)
	}

	req = httptest.NewRequest("POST", "/items/1", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Errorf("POST: got %d, want 201", w.Code)
	}

	req = httptest.NewRequest("DELETE", "/items/1", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("DELETE: got %d, want 200", w.Code)
	}

	req = httptest.NewRequest("PUT", "/items/1", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("PUT not registered: got %d, want 404", w.Code)
	}
}

func TestServer_RegexConstraint(t *testing.T) {
	s := New()
	s.MapGet("/files/{name:regex(^[a-z]+\\.txt$)}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"name": ctx.Params["name"]})
	})

	req := httptest.NewRequest("GET", "/files/readme.txt", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("regex valid: got %d, want 200, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/files/README.TXT", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("regex invalid: got %d, want 404", w.Code)
	}
}