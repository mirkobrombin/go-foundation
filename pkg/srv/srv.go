// Package srv provides a minimal API server with routing, middleware, and model binding.
package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/options"
)

// Context holds the request state for a single HTTP request.
type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Params  map[string]string
	Ctx     context.Context
	values  map[string]any
}

// Set stores a key-value pair in the request context.
func (c *Context) Set(key string, val any) {
	if c.values == nil {
		c.values = make(map[string]any)
	}
	c.values[key] = val
}

// Get retrieves a value from the request context by key.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.values[key]
	return v, ok
}

// JSON writes a JSON response with the given status code.
func (c *Context) JSON(code int, v any) error {
	c.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Response.WriteHeader(code)
	return json.NewEncoder(c.Response).Encode(v)
}

// String writes a plain text response with the given status code.
func (c *Context) String(code int, s string) {
	c.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Response.WriteHeader(code)
	c.Response.Write([]byte(s))
}

// Bind decodes the request body into v based on the Content-Type header.
func (c *Context) Bind(v any) error {
	ct := c.Request.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		return json.NewDecoder(c.Request.Body).Decode(v)
	}
	return fmt.Errorf("srv: unsupported content type: %s", ct)
}

// HandlerFunc is the handler signature for srv routes.
type HandlerFunc func(*Context) error

// Middleware wraps a HandlerFunc, returning a new HandlerFunc.
type Middleware func(HandlerFunc) HandlerFunc

type route struct {
	method  string
	path    string
	handler HandlerFunc
}

// Server is a minimal API HTTP server with routing and middleware support.
type Server struct {
	middleware []Middleware
	routes     []route
	groups     []*group
	mu         sync.RWMutex
	server     *http.Server
}

// Option configures a Server.
type Option = options.Option[Server]

// New creates a new Server with optional configuration.
func New(opts ...Option) *Server {
	s := &Server{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type group struct {
	prefix     string
	middleware []Middleware
	server     *Server
}

// Group creates a route group with a common prefix and optional middleware.
func (s *Server) Group(prefix string, mw ...Middleware) *group {
	g := &group{prefix: prefix, middleware: mw, server: s}
	s.mu.Lock()
	s.groups = append(s.groups, g)
	s.mu.Unlock()
	return g
}

// MapGet registers a GET route in the group.
func (g *group) MapGet(path string, handler HandlerFunc) {
	g.server.MapGet(g.prefix+path, handler, g.middleware...)
}

// MapPost registers a POST route in the group.
func (g *group) MapPost(path string, handler HandlerFunc) {
	g.server.MapPost(g.prefix+path, handler, g.middleware...)
}

// MapPut registers a PUT route in the group.
func (g *group) MapPut(path string, handler HandlerFunc) {
	g.server.MapPut(g.prefix+path, handler, g.middleware...)
}

// MapDelete registers a DELETE route in the group.
func (g *group) MapDelete(path string, handler HandlerFunc) {
	g.server.MapDelete(g.prefix+path, handler, g.middleware...)
}

// Use registers global middleware on the server.
func (s *Server) Use(mw Middleware) {
	s.mu.Lock()
	s.middleware = append(s.middleware, mw)
	s.mu.Unlock()
}

// MapGet registers a GET route at the given path.
func (s *Server) MapGet(path string, handler HandlerFunc, mw ...Middleware) {
	s.addRoute("GET", path, handler, mw...)
}

// MapPost registers a POST route at the given path.
func (s *Server) MapPost(path string, handler HandlerFunc, mw ...Middleware) {
	s.addRoute("POST", path, handler, mw...)
}

// MapPut registers a PUT route at the given path.
func (s *Server) MapPut(path string, handler HandlerFunc, mw ...Middleware) {
	s.addRoute("PUT", path, handler, mw...)
}

// MapDelete registers a DELETE route at the given path.
func (s *Server) MapDelete(path string, handler HandlerFunc, mw ...Middleware) {
	s.addRoute("DELETE", path, handler, mw...)
}

func (s *Server) addRoute(method, path string, handler HandlerFunc, mw ...Middleware) {
	s.mu.Lock()
	s.routes = append(s.routes, route{method: method, path: path, handler: chainMiddleware(handler, append(s.middleware, mw...)...)})
	s.mu.Unlock()
}

func chainMiddleware(h HandlerFunc, mw ...Middleware) HandlerFunc {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// ServeHTTP implements http.Handler, dispatching requests through global middleware and routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	globalMW := make([]Middleware, len(s.middleware))
	copy(globalMW, s.middleware)
	s.mu.RUnlock()

	h := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveRoutes(w, r)
	}))

	for i := len(globalMW) - 1; i >= 0; i-- {
		mw := globalMW[i]
		next := h
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := &Context{Request: r, Response: w, Ctx: r.Context(), values: make(map[string]any)}
			responded := false
			nextFn := func(c *Context) error {
				responded = true
				next.ServeHTTP(c.Response, c.Request)
				return nil
			}
			if err := mw(nextFn)(ctx); err != nil {
				if !responded {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(statusFromError(err))
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				}
			}
		})
	}
	h.ServeHTTP(w, r)
}

func (s *Server) serveRoutes(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	routes := make([]route, len(s.routes))
	copy(routes, s.routes)
	s.mu.RUnlock()

	for _, rt := range routes {
		if rt.method != r.Method {
			continue
		}
		params, ok := matchRoute(rt.path, r.URL.Path)
		if !ok {
			continue
		}

		ctx := &Context{
			Request:  r,
			Response: w,
			Params:  params,
			Ctx:     r.Context(),
		}

		if err := rt.handler(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

func matchRoute(pattern, path string) (map[string]string, bool) {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := make(map[string]string)
	for i := 0; i < len(patternParts); i++ {
		if strings.HasPrefix(patternParts[i], "{") && strings.HasSuffix(patternParts[i], "}") {
			paramName := patternParts[i][1 : len(patternParts[i])-1]
			params[paramName] = pathParts[i]
		} else if patternParts[i] != pathParts[i] {
			return nil, false
		}
	}
	return params, true
}

// ListenAndServe starts the server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	s.mu.Lock()
	s.server = &http.Server{Addr: addr, Handler: s}
	s.mu.Unlock()
	return s.server.ListenAndServe()
}

// ListenAndServeTLS starts the server with TLS on the given address.
func (s *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	s.mu.Lock()
	s.server = &http.Server{Addr: addr, Handler: s}
	s.mu.Unlock()
	return s.server.ListenAndServeTLS(certFile, keyFile)
}

//Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()
	if srv == nil {
		return fmt.Errorf("srv: server not started")
	}
	return srv.Shutdown(ctx)
}

// JSON writes a JSON response using the context.
func JSON(ctx *Context, code int, v any) error {
	return ctx.JSON(code, v)
}

// Error creates an HTTP error with a status code and message.
func Error(code int, msg string) error {
	return &httpError{Code: code, Message: msg}
}

type httpError struct {
	Code    int
	Message string
}

func (e *httpError) Error() string {
	return e.Message
}

// HTTPCode returns the HTTP status code from the error.
func (e *httpError) HTTPCode() int {
	return e.Code
}

// Recovery returns middleware that catches panics and converts them to errors.
func Recovery() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next(ctx)
		}
	}
}

// Logger returns middleware that logs request method, path, duration, and status.
func Logger() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			fmt.Printf("%s %s %v %d\n", ctx.Request.Method, ctx.Request.URL.Path, time.Since(start), statusFromError(err))
			return err
		}
	}
}

// CORS returns middleware that sets CORS headers. Preflight OPTIONS requests get 204.
func CORS(allowOrigins ...string) Middleware {
	origins := "*"
	if len(allowOrigins) > 0 {
		origins = strings.Join(allowOrigins, ", ")
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			ctx.Response.Header().Set("Access-Control-Allow-Origin", origins)
			ctx.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			ctx.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if ctx.Request.Method == "OPTIONS" {
				ctx.Response.WriteHeader(http.StatusNoContent)
				return nil
			}
			return next(ctx)
		}
	}
}

// RequestID returns middleware that sets and propagates X-Request-ID.
func RequestID() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			id := ctx.Request.Header.Get("X-Request-ID")
			if id == "" {
				id = fmt.Sprintf("%d", time.Now().UnixNano())
			}
			ctx.Set("request_id", id)
			ctx.Response.Header().Set("X-Request-ID", id)
			return next(ctx)
		}
	}
}

// Compress returns middleware that sets gzip content encoding.
func Compress() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			ctx.Response.Header().Set("Content-Encoding", "gzip")
			return next(ctx)
		}
	}
}

// HealthEndpoint returns a handler that reports health status based on the checker function.
func HealthEndpoint(checker func(ctx context.Context) error) HandlerFunc {
	return func(ctx *Context) error {
		if err := checker(ctx.Ctx); err != nil {
			return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
		}
		return ctx.JSON(http.StatusOK, map[string]string{"status": "healthy"})
	}
}

func statusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if he, ok := err.(*httpError); ok {
		return he.Code
	}
	return http.StatusInternalServerError
}