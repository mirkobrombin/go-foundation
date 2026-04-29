// Package srv provides a minimal API server with routing, middleware, and model binding.
package srv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
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
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if err := c.Request.ParseForm(); err != nil {
			return fmt.Errorf("srv: cannot parse form: %w", err)
		}
		return bindForm(c.Request.Form, v)
	}
	return fmt.Errorf("srv: unsupported content type: %s", ct)
}

func bindForm(form map[string][]string, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("srv: form binding requires pointer to struct")
	}
	elem := val.Elem()
	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		if !field.CanSet() {
			continue
		}
		fieldName := elem.Type().Field(i).Name
		jsonTag := elem.Type().Field(i).Tag.Get("json")
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" && parts[0] != "-" {
				fieldName = parts[0]
			}
		}
		if vals, ok := form[fieldName]; ok && len(vals) > 0 {
			setFieldValue(field, vals[0])
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, value string) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			field.SetInt(n)
		}
	case reflect.Float32, reflect.Float64:
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			field.SetFloat(n)
		}
	case reflect.Bool:
		if b, err := strconv.ParseBool(value); err == nil {
			field.SetBool(b)
		}
	}
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
	tree       *radixTree
}

// Option configures a Server.
type Option = options.Option[Server]

// New creates a new Server with optional configuration.
func New(opts ...Option) *Server {
	s := &Server{
		tree: newRadixTree(),
	}
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
	chained := chainMiddleware(handler, mw...)
	s.mu.Lock()
	s.routes = append(s.routes, route{method: method, path: path, handler: chained})
	s.tree.insert(method, path, chained)
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

	ctx := &Context{Request: r, Response: w, Ctx: r.Context()}
	ctx.values = make(map[string]any)

	handler := func(c *Context) error {
		s.serveRoutesWithContext(c)
		return nil
	}

	wrapped := chainMiddleware(handler, globalMW...)
	if err := wrapped(ctx); err != nil {
		ctx.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
		ctx.Response.WriteHeader(statusFromError(err))
		json.NewEncoder(ctx.Response).Encode(map[string]string{"error": err.Error()})
	}
}

func (s *Server) serveRoutesWithContext(ctx *Context) {
	handler, params := s.tree.lookup(ctx.Request.Method, ctx.Request.URL.Path)
	if handler == nil {
		ctx.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
		ctx.Response.WriteHeader(http.StatusNotFound)
		json.NewEncoder(ctx.Response).Encode(map[string]string{"error": "not found"})
		return
	}

	ctx.Params = params

	if err := handler(ctx); err != nil {
		ctx.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
		ctx.Response.WriteHeader(statusFromError(err))
		json.NewEncoder(ctx.Response).Encode(map[string]string{"error": err.Error()})
	}
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

type panicError struct {
	recovered   any
	stack       string
}

func (e *panicError) Error() string {
	return fmt.Sprintf("panic: %v\n%s", e.recovered, e.stack)
}

func (e *panicError) HTTPCode() int {
	return http.StatusInternalServerError
}

func (e *panicError) Unwrap() error {
	if err, ok := e.recovered.(error); ok {
		return err
	}
	return nil
}

// Recovery returns middleware that catches panics and converts them to errors with stack traces.
func Recovery() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = &panicError{
						recovered: r,
						stack:     string(debug.Stack()),
					}
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
	for {
		if he, ok := err.(interface{ HTTPCode() int }); ok {
			return he.HTTPCode()
		}
		if e := errors.Unwrap(err); e != nil {
			err = e
			continue
		}
		break
	}
	return http.StatusInternalServerError
}