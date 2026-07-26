package httpx

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func cloneRequest(r *http.Request) *http.Request {
	cloned := r.Clone(r.Context())
	cloned.Header = make(http.Header, len(r.Header))
	for k, v := range r.Header {
		cloned.Header[k] = append([]string(nil), v...)
	}
	return cloned
}

// Header returns a Middleware that sets the specified header on every request.
func Header(key, value string) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if !sameOrigin(r.URL, originalRequestURL(r)) {
				return next.RoundTrip(r)
			}
			cloned := cloneRequest(r)
			cloned.Header.Set(key, value)
			return next.RoundTrip(cloned)
		})
	}
}

func originalRequestURL(request *http.Request) *url.URL {
	if request == nil {
		return nil
	}
	original := request.URL
	for response := request.Response; response != nil && response.Request != nil; response = response.Request.Response {
		original = response.Request.URL
	}
	return original
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}

// Logging returns a Middleware that logs each request and its duration.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.RoundTrip(r)
			dur := time.Since(start)
			requestURL := safeLogURL(r.URL)
			if err != nil {
				logger.Error("http request failed", "method", r.Method, "url", requestURL, "duration", dur, "error", err)
			} else {
				logger.Info("http request", "method", r.Method, "url", requestURL, "status", resp.StatusCode, "duration", dur)
			}
			return resp, err
		})
	}
}

func safeLogURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	safe := *value
	safe.User = nil
	safe.RawQuery = ""
	safe.ForceQuery = false
	safe.Fragment = ""
	return safe.String()
}

// RequestID returns a Middleware that injects a unique request ID into the given header if absent.
func RequestID(header string) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get(header) == "" {
				cloned := cloneRequest(r)
				cloned.Header.Set(header, newRequestID())
				return next.RoundTrip(cloned)
			}
			return next.RoundTrip(r)
		})
	}
}

func newRequestID() string {
	return time.Now().Format("20060102150405.000000")
}
