package httpx

import (
	"log/slog"
	"net/http"
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

func Header(key, value string) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			cloned := cloneRequest(r)
			cloned.Header.Set(key, value)
			return next.RoundTrip(cloned)
		})
	}
}

func Logging(logger *slog.Logger) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.RoundTrip(r)
			dur := time.Since(start)
			if err != nil {
				logger.Error("http request failed", "method", r.Method, "url", r.URL.String(), "duration", dur, "error", err)
			} else {
				logger.Info("http request", "method", r.Method, "url", r.URL.String(), "status", resp.StatusCode, "duration", dur)
			}
			return resp, err
		})
	}
}

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
