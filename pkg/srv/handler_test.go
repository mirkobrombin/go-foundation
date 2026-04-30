package srv

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/di"
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

	s.RegisterHandler(&PingEndpoint{}, container)

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

func TestRegisterHandler_WithQuery(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	container, _ := b.Build()

	s.RegisterHandler(&PingEndpoint{}, container)

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

type DiEndpoint struct {
	Meta  struct{} `method:"GET" path:"/greet"`
	Name  string   `query:"name" default:"world"`
	Greet string
}

func (e *DiEndpoint) Handle(_ context.Context) (any, error) {
	return map[string]string{"greeting": "hello " + e.Name}, nil
}

func TestRegisterHandler_WithDI(t *testing.T) {
	s := New()
	b := di.NewBuilder()
	b.Provide("Greet", "custom-greeting")
	container, _ := b.Build()

	s.RegisterHandler(&DiEndpoint{}, container)

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

	s.RegisterHandler(&NoReturnEndpoint{}, container)

	req := httptest.NewRequest("DELETE", "/item", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
}