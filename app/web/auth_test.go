package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/core/auth"
)

var authTestSecret = []byte("0123456789abcdef0123456789abcdef")

func TestAuth_MissingHeader(t *testing.T) {
	s := New()
	s.Use(Auth(authTestSecret))
	s.MapGet("/api", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"ok": "yes"})
	})

	req := httptest.NewRequest("GET", "/api", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	s := New()
	s.Use(Auth(authTestSecret))
	s.MapGet("/api", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"ok": "yes"})
	})

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer badtoken")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestAuth_ValidToken(t *testing.T) {
	s := New()
	s.Use(Auth(authTestSecret))
	s.MapGet("/api", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"ok": "yes"})
	})

	token, _ := auth.SignToken(auth.Payload{Sub: "user1", Exp: 9999999999}, authTestSecret)
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestAuth_RejectsShortSecretAtConfiguration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Auth() accepted a short HMAC secret")
		}
	}()
	Auth([]byte("short"))
}
