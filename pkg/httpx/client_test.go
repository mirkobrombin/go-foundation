package httpx_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/resiliency"
	"github.com/mirkobrombin/go-foundation/pkg/httpx"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}
}

func TestHeaderMiddlewareAddsHeaderWithoutMutatingRequest(t *testing.T) {
	originalHeader := http.Header{}
	originalHeader.Set("X-Original", "1")

	var seenHeader string
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		seenHeader = r.Header.Get("X-Test")
		if r.Header.Get("X-Original") != "1" {
			t.Fatalf("RoundTrip() original header missing")
		}
		return okResponse(), nil
	})}, httpx.Header("X-Test", "1"))

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header = originalHeader

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if seenHeader != "1" {
		t.Fatalf("RoundTrip() saw header %q, want %q", seenHeader, "1")
	}
	if req.Header.Get("X-Test") != "" {
		t.Fatalf("request header mutated unexpectedly")
	}
}

// TestDoReturnsResponse verifies that Do returns the actual response and not nil.
// This was broken in the original implementation due to Go's left-to-right
// evaluation of return expressions: `return resp, fn()` read resp (nil) before
// fn() had a chance to set it.
func TestDoReturnsResponse(t *testing.T) {
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return okResponse(), nil
	})})

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if resp == nil {
		t.Fatal("Do() returned nil response, want non-nil")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Do() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClientWithRetryRetriesTransportErrors(t *testing.T) {
	var attempts int
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("temporary")
		}
		return okResponse(), nil
	})})
	client.WithRetry(
		resiliency.WithAttempts(3),
		resiliency.WithDelay(time.Nanosecond, time.Nanosecond),
	)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if resp == nil {
		t.Fatal("Do() returned nil response after retry, want non-nil")
	}
	defer resp.Body.Close()

	if attempts != 3 {
		t.Fatalf("Do() attempts = %d, want 3", attempts)
	}
}

// TestClientWithRetryResetsBody verifies that each retry attempt receives the
// full request body. Previously, req.Body was consumed on the first RoundTrip
// and subsequent attempts would silently send an empty body.
func TestClientWithRetryResetsBody(t *testing.T) {
	var bodies []string
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) < 3 {
			return nil, errors.New("temporary")
		}
		return okResponse(), nil
	})})
	client.WithRetry(
		resiliency.WithAttempts(3),
		resiliency.WithDelay(time.Nanosecond, time.Nanosecond),
	)

	payload := []byte("hello world")
	req, err := http.NewRequest(http.MethodPost, "https://example.com", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	// GetBody lets the client clone the body on each attempt.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	if _, err := client.Do(req); err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	for i, body := range bodies {
		if body != "hello world" {
			t.Fatalf("attempt %d received body %q, want %q", i+1, body, "hello world")
		}
	}
}

func TestClientWithBreakerStopsOpenCircuit(t *testing.T) {
	breaker := resiliency.NewCircuitBreaker(1, time.Hour)
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})})
	client.WithBreaker(breaker)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	if _, err := client.Do(req); err == nil {
		t.Fatalf("first Do() error = nil, want transport error")
	}

	if _, err := client.Do(req); !errors.Is(err, resiliency.ErrCircuitOpen) {
		t.Fatalf("second Do() error = %v, want ErrCircuitOpen", err)
	}
}

// TestDoRetryWithBodyRequiresGetBody verifies that Do fails fast when retry is
// configured, the request has a body, but req.GetBody is not set. Without this
// guard, retries would silently send an empty body after the first attempt
// consumed the reader.
func TestDoRetryWithBodyRequiresGetBody(t *testing.T) {
	var calls int
	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return okResponse(), nil
	})})
	client.WithRetry(
		resiliency.WithAttempts(3),
		resiliency.WithDelay(time.Nanosecond, time.Nanosecond),
	)

	req, err := http.NewRequest(http.MethodPost, "https://example.com", io.NopCloser(bytes.NewReader([]byte("payload"))))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	// Intentionally leave req.GetBody unset.

	_, err = client.Do(req)
	if err == nil {
		t.Fatal("Do() error = nil, want fail-fast error")
	}
	if !strings.Contains(err.Error(), "GetBody") {
		t.Fatalf("Do() error = %v, want message mentioning GetBody", err)
	}
	if calls != 0 {
		t.Fatalf("Do() made %d HTTP call(s), want 0", calls)
	}
}
// regardless of how many times Do is called.
func TestTransportBuiltOnce(t *testing.T) {
	var buildCount atomic.Int32

	countingMW := func(next http.RoundTripper) http.RoundTripper {
		buildCount.Add(1)
		return next
	}

	client := httpx.New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return okResponse(), nil
	})}, countingMW)

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do() call %d error = %v", i, err)
		}
		resp.Body.Close()
	}

	if n := buildCount.Load(); n != 1 {
		t.Fatalf("middleware built %d times, want 1", n)
	}
}
