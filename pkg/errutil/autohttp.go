package errutil

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

// AutoHTTP returns an HTTP middleware that recovers from panics and renders a
// rich stack trace (in dev mode, as plain text; in any case the full trace is
// logged to stderr).
func AutoHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				raw := debug.Stack()

				fmt.Fprintf(os.Stderr, "\n%s panic: %v %s\n", r.Method, r.URL.String(), rec)
				os.Stderr.Write(raw)
				fmt.Fprintln(os.Stderr)

				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)

				fmt.Fprintf(w, "500 – Internal Server Error\n\n")
				fmt.Fprintf(w, "panic: %v\n\n", rec)
				fmt.Fprintf(w, "Request: %s %s\n\n", r.Method, r.URL.String())

				lines := bytes.Split(raw, []byte("\n"))
				for _, l := range lines {
					str := string(bytes.TrimSpace(l))
					if str == "" || len(str) == 0 {
						continue
					}
					if startsWithAny(str, "goroutine ", "runtime.", "panic({", "pkg/errutil/") {
						continue
					}
					if containsAny(str, "runtime/", "runtime(") {
						continue
					}
					fmt.Fprintf(w, "  %s\n", str)
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func startsWithAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

func containsAny(s string, substrs ...string) bool {
	for _, ss := range substrs {
		if contains(s, ss) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && strings.Contains(s, substr)
}