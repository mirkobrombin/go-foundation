package srv

import (
	"net/http"

	"github.com/mirkobrombin/go-foundation/pkg/resiliency"
)

// RateLimit returns middleware that limits requests per client using a token bucket.
func RateLimit(rate, burst int) Middleware {
	rl := resiliency.NewRateLimiter(rate, burst)
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			if err := rl.Wait(ctx.Ctx); err != nil {
				return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			}
			return next(ctx)
		}
	}
}
