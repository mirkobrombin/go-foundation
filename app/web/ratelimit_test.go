package web

import (
	"net/http/httptest"
	"testing"
)

func TestRateLimitRejectsInvalidConfiguration(t *testing.T) {
	if _, err := RateLimit(0, 1); err == nil {
		t.Fatal("RateLimit() accepted a zero rate")
	}
	if _, err := RateLimit(1, 0); err == nil {
		t.Fatal("RateLimit() accepted a zero burst")
	}
}

func TestRateLimitSeparatesClientsWithoutBlocking(t *testing.T) {
	middleware, err := RateLimit(1, 1)
	if err != nil {
		t.Fatalf("RateLimit() error = %v", err)
	}
	server := New()
	server.Use(middleware)
	server.MapGet("/", func(ctx *Context) error {
		return ctx.JSON(200, map[string]bool{"ok": true})
	})

	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "192.0.2.1:1000"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("first client status = %d", response.Code)
	}

	request = httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "192.0.2.1:1001"
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 429 {
		t.Fatalf("limited client status = %d, want 429", response.Code)
	}

	request = httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "192.0.2.2:1000"
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("second client status = %d, want 200", response.Code)
	}
}
