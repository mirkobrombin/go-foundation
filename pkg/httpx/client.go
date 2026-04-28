package httpx

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/resiliency"
)

// Middleware wraps an http.RoundTripper.
type Middleware func(next http.RoundTripper) http.RoundTripper

// Client is an extensible HTTP client with middleware and optional resiliency hooks.
type Client struct {
	client *http.Client
	rt     http.RoundTripper
	mw     []Middleware

	retryOpts []func(*resiliency.RetryOptions)
	breaker   *resiliency.CircuitBreaker

	// builtTransport caches the composed middleware chain so it is only
	// assembled once rather than on every Do call.
	builtTransport http.RoundTripper
	buildOnce      sync.Once
}

// New creates a new Client wrapping c (or a default client with 15s timeout if nil).
// Middleware options (WithRetry, WithBreaker) must be configured before the first
// call to Do; the transport chain is assembled once on first use and cannot be
// modified afterward.
func New(c *http.Client, mw ...Middleware) *Client {
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}

	rt := c.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}

	return &Client{client: c, rt: rt, mw: mw}
}

// WithRetry attaches retry options to the client.
func (c *Client) WithRetry(opts ...func(*resiliency.RetryOptions)) *Client {
	c.retryOpts = opts
	return c
}

// WithBreaker attaches a circuit breaker to the client.
func (c *Client) WithBreaker(b *resiliency.CircuitBreaker) *Client {
	c.breaker = b
	return c
}

// transport returns the cached composed middleware chain, building it on first use.
func (c *Client) transport() http.RoundTripper {
	c.buildOnce.Do(func() {
		rt := c.rt
		for i := len(c.mw) - 1; i >= 0; i-- {
			rt = c.mw[i](rt)
		}
		c.builtTransport = rt
	})
	return c.builtTransport
}

// Do sends a request applying middleware and optional resiliency behaviors.
//
// When retry options are set, requests with a body must provide req.GetBody so
// the body can be re-read on each attempt. Requests without a body (e.g. GET)
// are retried unconditionally.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if len(c.retryOpts) > 0 && req.Body != nil && req.GetBody == nil {
		return nil, fmt.Errorf("httpx: retry requires req.GetBody to be set when request has a body")
	}

	rt := c.transport()
	var resp *http.Response

	fn := func() error {
		// Clone the request and reset the body for each attempt so retries
		// send the complete payload rather than an already-consumed reader.
		attempt := req.Clone(req.Context())
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return err
			}
			attempt.Body = body
		}
		r, err := rt.RoundTrip(attempt)
		resp = r
		return err
	}

	var err error

	if c.breaker != nil {
		if len(c.retryOpts) > 0 {
			err = c.breaker.Execute(func() error {
				return resiliency.Retry(req.Context(), fn, c.retryOpts...)
			})
		} else {
			err = c.breaker.Execute(fn)
		}
		return resp, err
	}

	if len(c.retryOpts) > 0 {
		err = resiliency.Retry(req.Context(), fn, c.retryOpts...)
		return resp, err
	}

	err = fn()
	return resp, err
}
