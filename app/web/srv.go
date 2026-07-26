// Package web provides a minimal API server with routing, middleware, and model binding.
package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/options"
)

const maxRequestBodySize = 1 << 20

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
)

// Context holds the request state for a single HTTP request.
type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Params   map[string]string
	Ctx      context.Context
	values   map[string]any
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
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Response.WriteHeader(code)
	_, err = c.Response.Write(data)
	return err
}

// String writes a plain text response with the given status code.
func (c *Context) String(code int, s string) error {
	c.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Response.WriteHeader(code)
	_, err := c.Response.Write([]byte(s))
	return err
}

// Bind decodes the request body into v based on the Content-Type header.
func (c *Context) Bind(v any) error {
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxRequestBodySize)
	ct := c.Request.Header.Get("Content-Type")
	contentType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return Error(http.StatusUnsupportedMediaType, fmt.Sprintf("web: unsupported content type: %s", ct))
	}
	if isJSONContentType(ct) {
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(v); err != nil {
			return requestBodyError(err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return Error(http.StatusBadRequest, "web: request body contains multiple JSON values")
			}
			return requestBodyError(err)
		}
		return nil
	}
	if contentType == "application/x-www-form-urlencoded" {
		if err := c.Request.ParseForm(); err != nil {
			return requestBodyError(err)
		}
		if err := bindForm(c.Request.Form, v); err != nil {
			return Error(http.StatusBadRequest, err.Error())
		}
		return nil
	}
	return Error(http.StatusUnsupportedMediaType, fmt.Sprintf("web: unsupported content type: %s", ct))
}

func bindForm(form map[string][]string, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("web: form binding requires pointer to struct")
	}
	elem := val.Elem()
	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		if !field.CanSet() {
			continue
		}
		fieldName, ok := elem.Type().Field(i).Tag.Lookup("form")
		if !ok || fieldName == "" || fieldName == "-" {
			continue
		}
		if vals, ok := form[fieldName]; ok && len(vals) > 0 {
			if err := setFieldValue(field, vals[0]); err != nil {
				return fmt.Errorf("web: form field %s: %w", fieldName, err)
			}
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		field.SetFloat(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(b)
	default:
		return fmt.Errorf("unsupported type %s", field.Kind())
	}
	return nil
}

func requestBodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return Error(http.StatusRequestEntityTooLarge, "web: request body exceeds 1 MiB")
	}
	return Error(http.StatusBadRequest, fmt.Sprintf("web: invalid request body: %v", err))
}

// HandlerFunc is the handler signature for web routes.
type HandlerFunc func(*Context) error

// Middleware wraps a HandlerFunc, returning a new HandlerFunc.
type Middleware func(HandlerFunc) HandlerFunc

type route struct {
	method  string
	path    string
	handler HandlerFunc
}

// RouteInfo describes a registered route.
type RouteInfo struct {
	Method string
	Path   string
}

// RouteDefinition describes a raw HTTP route for batch registration.
type RouteDefinition struct {
	Method     string
	Path       string
	Handler    HandlerFunc
	Middleware []Middleware
}

// Server is a minimal API HTTP server with routing and middleware support.
type Server struct {
	middleware []Middleware
	routes     []route
	groups     []*group
	mu         sync.RWMutex
	server     *http.Server
	tree       *radixTree
	started    chan struct{}
}

// Option configures a Server.
type Option = options.Option[Server]

// New creates a new Server with optional configuration.
func New(opts ...Option) *Server {
	s := &Server{
		tree:    newRadixTree(),
		started: make(chan struct{}),
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
func (g *group) MapGet(path string, handler HandlerFunc) error {
	return g.server.MapGet(g.prefix+path, handler, g.middleware...)
}

// MapPost registers a POST route in the group.
func (g *group) MapPost(path string, handler HandlerFunc) error {
	return g.server.MapPost(g.prefix+path, handler, g.middleware...)
}

// MapPut registers a PUT route in the group.
func (g *group) MapPut(path string, handler HandlerFunc) error {
	return g.server.MapPut(g.prefix+path, handler, g.middleware...)
}

// MapDelete registers a DELETE route in the group.
func (g *group) MapDelete(path string, handler HandlerFunc) error {
	return g.server.MapDelete(g.prefix+path, handler, g.middleware...)
}

// Use registers global middleware on the server.
func (s *Server) Use(mw Middleware) {
	s.mu.Lock()
	s.middleware = append(s.middleware, mw)
	s.mu.Unlock()
}

// MapGet registers a GET route at the given path.
func (s *Server) MapGet(path string, handler HandlerFunc, mw ...Middleware) error {
	return s.addRoute("GET", path, handler, mw...)
}

// MapPost registers a POST route at the given path.
func (s *Server) MapPost(path string, handler HandlerFunc, mw ...Middleware) error {
	return s.addRoute("POST", path, handler, mw...)
}

// MapPut registers a PUT route at the given path.
func (s *Server) MapPut(path string, handler HandlerFunc, mw ...Middleware) error {
	return s.addRoute("PUT", path, handler, mw...)
}

// MapDelete registers a DELETE route at the given path.
func (s *Server) MapDelete(path string, handler HandlerFunc, mw ...Middleware) error {
	return s.addRoute("DELETE", path, handler, mw...)
}

func (s *Server) addRoute(method, path string, handler HandlerFunc, mw ...Middleware) error {
	return s.RegisterRoutes(RouteDefinition{
		Method:     method,
		Path:       path,
		Handler:    handler,
		Middleware: mw,
	})
}

// RegisterRoutes validates and registers raw routes as one operation.
func (s *Server) RegisterRoutes(definitions ...RouteDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prepared := make([]route, 0, len(definitions))
	check := newRadixTree()
	for _, existing := range s.routes {
		if err := check.insert(existing.method, existing.path, existing.handler); err != nil {
			return err
		}
	}
	for _, definition := range definitions {
		chained := chainMiddleware(definition.Handler, definition.Middleware...)
		if err := check.insert(definition.Method, definition.Path, chained); err != nil {
			return err
		}
		prepared = append(prepared, route{
			method:  definition.Method,
			path:    definition.Path,
			handler: chained,
		})
	}
	for _, item := range prepared {
		if err := s.tree.insert(item.method, item.path, item.handler); err != nil {
			return err
		}
		s.routes = append(s.routes, item)
	}
	return nil
}

// Routes returns the registered route metadata.
func (s *Server) Routes() []RouteInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]RouteInfo, 0, len(s.routes))
	for _, route := range s.routes {
		routes = append(routes, RouteInfo{Method: route.method, Path: route.path})
	}
	return routes
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
		return s.serveRoutesWithContext(c)
	}

	wrapped := chainMiddleware(handler, globalMW...)
	if err := wrapped(ctx); err != nil {
		ctx.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
		ctx.Response.WriteHeader(statusFromError(err))
		json.NewEncoder(ctx.Response).Encode(map[string]string{"error": publicErrorMessage(err)})
	}
}

func (s *Server) serveRoutesWithContext(ctx *Context) error {
	s.mu.RLock()
	handler, params := s.tree.lookup(ctx.Request.Method, ctx.Request.URL.Path)
	s.mu.RUnlock()
	if handler == nil {
		return Error(http.StatusNotFound, "not found")
	}

	ctx.Params = params
	return handler(ctx)
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
	if err := s.runDoctor(); err != nil {
		return err
	}
	return s.serve(addr, "", "")
}

// ListenAndServeTLS starts the server with TLS on the given address.
func (s *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	if err := s.runDoctor(); err != nil {
		return err
	}

	return s.serve(addr, certFile, keyFile)
}

func (s *Server) serve(addr, certFile, keyFile string) error {
	server, err := s.prepareHTTPServer(addr)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Lock()
		if s.server == server {
			s.server = nil
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	close(s.started)
	s.mu.Unlock()
	if certFile != "" {
		return server.ServeTLS(listener, certFile, keyFile)
	}
	return server.Serve(listener)
}

func (s *Server) prepareHTTPServer(addr string) (*http.Server, error) {
	server := s.httpServer(addr)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil, fmt.Errorf("web: server already started")
	}
	s.server = server
	if s.started == nil {
		s.started = make(chan struct{})
	}
	return server, nil
}

// Started reports when the underlying HTTP server has been prepared.
func (s *Server) Started() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started == nil {
		s.started = make(chan struct{})
		if s.server != nil {
			close(s.started)
		}
	}
	return s.started
}

func (s *Server) httpServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    maxRequestBodySize,
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	server := s.server
	s.mu.RUnlock()
	if server == nil {
		return fmt.Errorf("web: server not started")
	}
	return server.Shutdown(ctx)
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
	recovered any
	stack     string
}

func (e *panicError) Error() string {
	return "internal server error"
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

func (e *panicError) Stack() string {
	return e.stack
}

func (e *panicError) Recovered() any {
	return e.recovered
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
			fmt.Printf("%s %q %v %d\n", ctx.Request.Method, ctx.Request.URL.Path, time.Since(start), statusFromError(err))
			return err
		}
	}
}

// CORS returns middleware that sets CORS headers. Preflight OPTIONS requests get 204.
func CORS(allowOrigins ...string) Middleware {
	allowAny := len(allowOrigins) == 0
	allowed := make(map[string]struct{}, len(allowOrigins))
	for _, origin := range allowOrigins {
		if origin == "*" {
			allowAny = true
		}
		allowed[origin] = struct{}{}
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			origin := ctx.Request.Header.Get("Origin")
			if allowAny {
				ctx.Response.Header().Set("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowed[origin]; ok && origin != "" {
				ctx.Response.Header().Set("Access-Control-Allow-Origin", origin)
				ctx.Response.Header().Add("Vary", "Origin")
			}
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

// Compress returns middleware that writes gzip responses when requested.
func Compress() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			if !acceptsGzip(ctx.Request.Header.Get("Accept-Encoding")) {
				return next(ctx)
			}

			original := ctx.Response
			writer := &gzipResponseWriter{
				ResponseWriter: original,
				writer:         gzip.NewWriter(original),
			}
			ctx.Response = writer
			defer func() {
				ctx.Response = original
			}()

			err := next(ctx)
			if writer.compressed {
				if closeErr := writer.writer.Close(); err == nil {
					err = closeErr
				}
			}
			return err
		}
	}
}

func acceptsGzip(value string) bool {
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			name, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(name, "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return false
			}
			quality = parsed
		}
		return quality > 0
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	compressed  bool
	passthrough bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if status < http.StatusOK || status == http.StatusNoContent || status == http.StatusNotModified {
		w.passthrough = true
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.prepare()
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(content []byte) (int, error) {
	if w.passthrough {
		return w.ResponseWriter.Write(content)
	}
	w.prepare()
	return w.writer.Write(content)
}

func (w *gzipResponseWriter) prepare() {
	if w.compressed {
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	w.Header().Add("Vary", "Accept-Encoding")
	w.compressed = true
}

// HealthEndpoint returns a handler that reports health status based on the checker function.
func HealthEndpoint(checker func(ctx context.Context) error) HandlerFunc {
	return func(ctx *Context) error {
		if err := checker(ctx.Ctx); err != nil {
			return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		}
		return ctx.JSON(http.StatusOK, map[string]string{"status": "healthy"})
	}
}

func publicErrorMessage(err error) string {
	var recovered *panicError
	if errors.As(err, &recovered) {
		return "internal server error"
	}
	var explicit *httpError
	if errors.As(err, &explicit) {
		return explicit.Message
	}
	if statusFromError(err) >= http.StatusInternalServerError {
		return "internal server error"
	}
	return err.Error()
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
