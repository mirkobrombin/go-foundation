package testutil

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/di"
	"github.com/mirkobrombin/go-foundation/pkg/srv"
)

// TestHost provides a DI container and test HTTP server.
type TestHost struct {
	Container *di.Container
	Server    *httptest.Server
	Client    *http.Client
	cleanup   []func()
}

// NewTestHost creates a TestHost with a DI container and test server.
func NewTestHost(setup func(b *di.Builder, app *srv.Server)) *TestHost {
	b := di.NewBuilder()
	app := srv.New()
	setup(b, app)

	container := b.Build()
	ts := httptest.NewServer(app)

	return &TestHost{
		Container: container,
		Server:    ts,
		Client:    ts.Client(),
	}
}

// Close shuts down the test server and runs cleanup functions.
func (h *TestHost) Close() {
	h.Server.Close()
	for _, f := range h.cleanup {
		f()
	}
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