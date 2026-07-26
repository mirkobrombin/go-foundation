package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
)

type PingEndpoint struct {
	Meta  struct{} `method:"GET" path:"/ping"`
	Times int      `query:"times" default:"1"`
}

type pingResponse struct {
	Message []string `json:"message"`
}

func (e *PingEndpoint) Handle(_ context.Context) (any, error) {
	out := make([]string, e.Times)
	for i := range out {
		out[i] = "pong"
	}
	return pingResponse{Message: out}, nil
}

func TestRegisterHandler_Basic(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	container, _ := b.Build()

	if err := s.RegisterHandler(&PingEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	var resp pingResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Message) != 1 {
		t.Errorf("messages = %d, want 1", len(resp.Message))
	}
}

func TestRegisterHandler_Routes(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	container, _ := b.Build()

	if err := s.RegisterHandler(&PingEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	routes := s.Routes()
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	if routes[0].Method != "GET" || routes[0].Path != "/ping" {
		t.Errorf("route = %s %s, want GET /ping", routes[0].Method, routes[0].Path)
	}
}

func TestRegisterHandler_WithQuery(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	container, _ := b.Build()

	if err := s.RegisterHandler(&PingEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/ping?times=3", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp pingResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Message) != 3 {
		t.Errorf("messages = %d, want 3", len(resp.Message))
	}
}

func TestRegisterHandler_InvalidQueryReturnsBadRequest(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()

	if err := s.RegisterHandler(&PingEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/ping?times=invalid", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

type DiEndpoint struct {
	Meta  struct{} `method:"GET" path:"/greet"`
	Name  string   `query:"name" default:"world"`
	Greet string   `inject:"greet"`
}

func (e *DiEndpoint) Handle(_ context.Context) (any, error) {
	return map[string]string{"greeting": "hello " + e.Name}, nil
}

func TestRegisterHandler_WithDI(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	b.Provide("greet", "custom-greeting")
	container, _ := b.Build()

	if err := s.RegisterHandler(&DiEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/greet?name=alice", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

type NoReturnEndpoint struct {
	Meta struct{} `method:"DELETE" path:"/item"`
}

func (e *NoReturnEndpoint) Handle(_ context.Context) (any, error) {
	return nil, nil
}

func TestRegisterHandler_NilReturn(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	container, _ := b.Build()

	if err := s.RegisterHandler(&NoReturnEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("DELETE", "/item", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestRegisterHandler_RejectsMissingDependency(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()

	if err := s.RegisterHandler(&DiEndpoint{}, container); err == nil {
		t.Fatal("RegisterHandler() accepted a missing dependency")
	}
}

func TestRegisterHandler_RejectsDuplicateRoute(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()

	if err := s.RegisterHandler(&PingEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}
	if err := s.RegisterHandler(&PingEndpoint{}, container); err == nil {
		t.Fatal("RegisterHandler() accepted a duplicate route")
	}
}

func TestRegisterDefinition(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()

	err := s.RegisterDefinition(HandlerDefinition{
		Method: "GET",
		Path:   "/static",
		New: func() Handler {
			return &PingEndpoint{}
		},
	}, container)
	if err != nil {
		t.Fatalf("RegisterDefinition() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/static", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

type BodyEndpoint struct {
	Meta struct{} `method:"POST" path:"/body"`
	Body struct {
		Name string `json:"name"`
	} `body:"json"`
}

func (e *BodyEndpoint) Handle(_ context.Context) (any, error) {
	return e.Body, nil
}

func TestRegisterHandler_RejectsUnknownBodyField(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()
	if err := s.RegisterHandler(&BodyEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("POST", "/body", strings.NewReader(`{"name":"Ada","admin":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRegisterHandler_RejectsOversizedBody(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()
	if err := s.RegisterHandler(&BodyEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("POST", "/body", strings.NewReader(strings.Repeat("x", maxRequestBodySize+1)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 413 {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

func TestRegisterHandler_RejectsWrongBodyContentType(t *testing.T) {
	s := New()
	container := di.NewBuilder().MustBuild()
	if err := s.RegisterHandler(&BodyEndpoint{}, container); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	req := httptest.NewRequest("POST", "/body", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 415 {
		t.Fatalf("status = %d, want 415", w.Code)
	}
}
