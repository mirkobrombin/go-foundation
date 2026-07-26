package apptest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/app/web"
)

// TestHost provides a DI container and test HTTP server.
type TestHost struct {
	Container *di.Container
	Server    *httptest.Server
	Client    *http.Client
	cleanup   []func()
}

// NewTestHost builds dependencies before registering routes against the final container.
func NewTestHost(
	configure func(*di.Builder),
	register func(*web.Server, *di.Container) error,
) *TestHost {
	builder := di.NewBuilder()
	server := web.New()
	if configure != nil {
		configure(builder)
	}

	container, err := builder.Build()
	if err != nil {
		panic(err)
	}
	if register != nil {
		if err := register(server, container); err != nil {
			_ = container.Close()
			panic(err)
		}
	}
	testServer := httptest.NewServer(server)

	return &TestHost{
		Container: container,
		Server:    testServer,
		Client:    testServer.Client(),
	}
}

// Close shuts down the test server and runs cleanup functions.
func (h *TestHost) Close() {
	h.Server.Close()
	_ = h.Container.Close()
	for _, f := range h.cleanup {
		f()
	}
}

// Cleanup registers a function to run when the host closes.
func (h *TestHost) Cleanup(fn func()) {
	h.cleanup = append(h.cleanup, fn)
}

// URL returns the full URL for the given path.
func (h *TestHost) URL(path string) string {
	return h.Server.URL + path
}

// Get issues a GET request and returns a TestResponse.
func (h *TestHost) Get(path string) *TestResponse {
	resp, err := h.Client.Get(h.URL(path))
	if err != nil {
		return &TestResponse{Error: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return &TestResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    resp.Header,
	}
}

// Post issues a POST request and returns a TestResponse.
func (h *TestHost) Post(path string, contentType string, body io.Reader) *TestResponse {
	resp, err := h.Client.Post(h.URL(path), contentType, body)
	if err != nil {
		return &TestResponse{Error: err}
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	return &TestResponse{
		StatusCode: resp.StatusCode,
		Body:       responseBody,
		Headers:    resp.Header,
	}
}

// Resolve retrieves a typed dependency from the container.
func Resolve[T any](h *TestHost) T {
	return di.ResolveType[T](h.Container)
}

// TestResponse holds an HTTP response for test assertions.
type TestResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Error      error
}

// Decode unmarshals the response body into v.
func (r *TestResponse) Decode(v any) error {
	return json.Unmarshal(r.Body, v)
}

// String returns the response body as a string.
func (r *TestResponse) String() string {
	return string(r.Body)
}

// FakeLogger collects log entries for test assertions.
type FakeLogger struct {
	Entries []string
}

// NewFakeLogger creates a FakeLogger.
func NewFakeLogger() *FakeLogger {
	return &FakeLogger{}
}

// AssertLogged fails the test if msg was not logged.
func (l *FakeLogger) AssertLogged(t *testing.T, msg string) {
	t.Helper()
	found := false
	for _, e := range l.Entries {
		if e == msg {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected log %q, not found", msg)
	}
}

// TestContext holds a context and variable map for test use.
type TestContext struct {
	Ctx  context.Context
	Vars map[string]any
}

// NewTestContext creates a TestContext with a background context.
func NewTestContext() *TestContext {
	return &TestContext{
		Ctx:  context.Background(),
		Vars: make(map[string]any),
	}
}
