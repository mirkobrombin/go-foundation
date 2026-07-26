package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func TestServer_RecoveryDoesNotExposePanic(t *testing.T) {
	s := New()
	s.Use(Recovery())
	s.MapGet("/panic", func(ctx *Context) error {
		panic("private failure detail")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "private failure detail") ||
		strings.Contains(w.Body.String(), "goroutine") {
		t.Fatalf("response exposes panic details: %s", w.Body.String())
	}
}

func TestServer_DoesNotExposeInternalError(t *testing.T) {
	s := New()
	s.MapGet("/failure", func(ctx *Context) error {
		return errors.New("database connection private detail")
	})

	req := httptest.NewRequest("GET", "/failure", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "database") {
		t.Fatalf("response exposes internal error: %s", w.Body.String())
	}
}

func TestServer_ExposesExplicitHTTPError(t *testing.T) {
	s := New()
	s.MapGet("/teapot", func(ctx *Context) error {
		return Error(418, "short and stout")
	})

	req := httptest.NewRequest("GET", "/teapot", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 418 {
		t.Fatalf("status = %d, want 418", w.Code)
	}
	if !strings.Contains(w.Body.String(), "short and stout") {
		t.Fatalf("response lost explicit error: %s", w.Body.String())
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

func TestServer_Routes(t *testing.T) {
	s := New()
	s.MapGet("/hello", func(ctx *Context) error {
		ctx.String(200, "ok")
		return nil
	})
	s.MapPost("/data", func(ctx *Context) error {
		ctx.String(201, "ok")
		return nil
	})

	routes := s.Routes()
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	if routes[0].Method != "GET" || routes[0].Path != "/hello" {
		t.Errorf("route[0] = %s %s, want GET /hello", routes[0].Method, routes[0].Path)
	}
	if routes[1].Method != "POST" || routes[1].Path != "/data" {
		t.Errorf("route[1] = %s %s, want POST /data", routes[1].Method, routes[1].Path)
	}
}

func TestServer_HTTPTimeoutDefaults(t *testing.T) {
	server := New().httpServer(":8080")
	if server.ReadHeaderTimeout <= 0 ||
		server.ReadTimeout <= 0 ||
		server.WriteTimeout <= 0 ||
		server.IdleTimeout <= 0 {
		t.Fatalf("HTTP timeouts are not configured: %#v", server)
	}
	if server.MaxHeaderBytes != maxRequestBodySize {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxRequestBodySize)
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

func TestServer_CORSSelectsMatchingOrigin(t *testing.T) {
	server := New()
	server.Use(CORS("https://one.example", "https://two.example"))
	if err := server.MapGet("/cors", func(ctx *Context) error {
		ctx.String(http.StatusOK, "ok")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/cors", nil)
	request.Header.Set("Origin", "https://two.example")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://two.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if !strings.Contains(response.Header().Get("Vary"), "Origin") {
		t.Fatalf("Vary = %q, want Origin", response.Header().Get("Vary"))
	}
}

func TestServer_CompressWritesGzip(t *testing.T) {
	s := New()
	s.Use(Compress())
	s.MapGet("/compressed", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"message": "hello"})
	})

	req := httptest.NewRequest("GET", "/compressed", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", w.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer reader.Close()
	var body map[string]string
	if err := json.NewDecoder(reader).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["message"] != "hello" {
		t.Fatalf("message = %q", body["message"])
	}
}

func TestServer_CompressRestoresWriterAfterPanic(t *testing.T) {
	s := New()
	s.Use(Recovery())
	s.Use(Compress())
	if err := s.MapGet("/panic", func(*Context) error {
		panic("private failure")
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if strings.Contains(w.Body.String(), "private failure") {
		t.Fatalf("response exposes panic details: %s", w.Body.String())
	}
}

func TestServer_CompressRequiresGzipEncodingToken(t *testing.T) {
	s := New()
	s.Use(Compress())
	if err := s.MapGet("/plain", func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"message": "hello"})
	}); err != nil {
		t.Fatal(err)
	}

	for _, encoding := range []string{"xgzip", "gzip;q=0"} {
		req := httptest.NewRequest("GET", "/plain", nil)
		req.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if got := w.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding for %q = %q, want empty", encoding, got)
		}
	}
}

func TestServer_CompressHandlesJSONEncodingError(t *testing.T) {
	s := New()
	s.Use(Compress())
	if err := s.MapGet("/invalid", func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, make(chan int))
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/invalid", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty on encoding error", got)
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

func TestServer_BindRejectsUnknownJSONField(t *testing.T) {
	s := New()
	s.MapPost("/data", func(ctx *Context) error {
		var body struct {
			Name string `json:"name"`
		}
		return ctx.Bind(&body)
	})

	req := httptest.NewRequest("POST", "/data", strings.NewReader(`{"name":"Ada","admin":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestServer_BindRejectsOversizedBody(t *testing.T) {
	s := New()
	s.MapPost("/data", func(ctx *Context) error {
		var body map[string]string
		return ctx.Bind(&body)
	})

	body := `{"key":"` + strings.Repeat("x", maxRequestBodySize) + `"}`
	req := httptest.NewRequest("POST", "/data", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 413 {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

func TestServer_BindRejectsUnsupportedMediaType(t *testing.T) {
	s := New()
	s.MapPost("/data", func(ctx *Context) error {
		var body map[string]string
		return ctx.Bind(&body)
	})

	req := httptest.NewRequest("POST", "/data", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 415 {
		t.Fatalf("status = %d, want 415", w.Code)
	}
}

func TestServer_FormBindUsesExplicitFields(t *testing.T) {
	s := New()
	s.MapPost("/form", func(ctx *Context) error {
		var body struct {
			Name    string `form:"name"`
			IsAdmin bool   `json:"-"`
		}
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		return ctx.JSON(200, body)
	})

	req := httptest.NewRequest(
		"POST",
		"/form",
		strings.NewReader("name=Ada&IsAdmin=true"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if strings.Contains(w.Body.String(), "true") {
		t.Fatalf("form bound an unlisted field: %s", w.Body.String())
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
	if err := s.MapGet("/static/{*filepath}", func(ctx *Context) error {
		return ctx.JSON(200, map[string]string{"path": ctx.Params["filepath"]})
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.MapPost("/static/{*filepath}", func(ctx *Context) error {
		ctx.Response.WriteHeader(http.StatusCreated)
		return nil
	}); err != nil {
		t.Fatalf("MapPost() for existing catch-all: %v", err)
	}

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

	req = httptest.NewRequest("POST", "/static/css/main.css", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("POST catch-all: got %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestServer_RouteLookupBacktracksFromStaticBranch(t *testing.T) {
	server := New()
	if err := server.MapGet("/users/new/details", func(ctx *Context) error {
		ctx.String(http.StatusOK, "static")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.MapGet("/users/{id}/settings", func(ctx *Context) error {
		ctx.String(http.StatusOK, ctx.Params["id"])
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/users/new/settings", nil),
	)
	if response.Code != http.StatusOK || response.Body.String() != "new" {
		t.Fatalf("response = (%d, %q), want (200, new)", response.Code, response.Body.String())
	}
}

func TestServer_RouteLookupBacktracksAcrossMethods(t *testing.T) {
	server := New()
	if err := server.MapGet("/items/static", func(ctx *Context) error {
		ctx.String(http.StatusOK, "static")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.MapPost("/items/{id}", func(ctx *Context) error {
		ctx.String(http.StatusCreated, ctx.Params["id"])
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/items/static", nil),
	)
	if response.Code != http.StatusCreated || response.Body.String() != "static" {
		t.Fatalf("response = (%d, %q), want (201, static)", response.Code, response.Body.String())
	}
}

func TestServer_RejectsMalformedCatchAllRoutes(t *testing.T) {
	for _, path := range []string{
		"/static/{*}",
		"/static/{*path}/suffix",
		"/static/{*path:int}",
		"/static/{{id}}",
		"/static/{id}/{id}",
		"/static/{bad-name}",
	} {
		t.Run(path, func(t *testing.T) {
			server := New()
			if err := server.MapGet(path, func(*Context) error { return nil }); err == nil {
				t.Fatalf("MapGet(%q) accepted a malformed catch-all", path)
			}
		})
	}
}

func TestServer_RejectsParameterCatchAllConflicts(t *testing.T) {
	handler := func(*Context) error { return nil }
	for _, register := range []func(*Server) error{
		func(server *Server) error {
			if err := server.MapGet("/items/{id}", handler); err != nil {
				return err
			}
			return server.MapPost("/items/{*rest}", handler)
		},
		func(server *Server) error {
			if err := server.MapGet("/items/{*rest}", handler); err != nil {
				return err
			}
			return server.MapPost("/items/{id}", handler)
		},
	} {
		if err := register(New()); err == nil {
			t.Fatal("router accepted conflicting parameter and catch-all routes")
		}
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

func TestServer_RejectsUnknownConstraint(t *testing.T) {
	s := New()
	if err := s.MapGet("/items/{id:innt}", func(ctx *Context) error {
		return nil
	}); err == nil {
		t.Fatal("MapGet() accepted an unknown constraint")
	}
}

func TestServer_RejectsInvalidRegexConstraint(t *testing.T) {
	s := New()
	if err := s.MapGet("/items/{id:regex([)}", func(ctx *Context) error {
		return nil
	}); err == nil {
		t.Fatal("MapGet() accepted an invalid regex constraint")
	}
}

func TestServer_RejectsAmbiguousParameterRoute(t *testing.T) {
	s := New()
	if err := s.MapGet("/items/{id}", func(ctx *Context) error {
		return nil
	}); err != nil {
		t.Fatalf("MapGet() error = %v", err)
	}
	if err := s.MapGet("/items/{name}", func(ctx *Context) error {
		return nil
	}); err == nil {
		t.Fatal("MapGet() accepted an ambiguous parameter route")
	}
}
