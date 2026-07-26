package errutil

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
)

// AutoHTTP returns an HTTP middleware that recovers from panics without
// exposing panic values, request secrets, or stack traces to the client.
func AutoHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				raw := debug.Stack()
				requestPath := ""
				if r.URL != nil {
					requestPath = r.URL.EscapedPath()
				}
				fmt.Fprintf(os.Stderr, "\n%s panic: %v %s\n", r.Method, rec, requestPath)
				os.Stderr.Write(raw)
				fmt.Fprintln(os.Stderr)

				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintln(w, "500 - Internal Server Error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
