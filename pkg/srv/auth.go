package srv

import (
	"net/http"
	"strings"

	"github.com/mirkobrombin/go-foundation/pkg/auth"
)

// Auth returns middleware that validates a Bearer token using auth.VerifyToken.
// A valid token is attached to the context via auth.Payload.
func Auth(secret []byte) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			h := ctx.Request.Header.Get("Authorization")
			if h == "" {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "missing Authorization header"})
			}

			parts := strings.SplitN(h, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid Authorization format"})
			}

			payload, err := auth.VerifyToken(parts[1], secret)
			if err != nil {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
			}

			ctx.Set("auth.payload", payload)
			return next(ctx)
		}
	}
}
