<div align="center">
  <img src="https://github.com/mirkobrombin/go-foundation/blob/main/logo.png?raw=true" height="128"/>
  <h1>go-foundation</h1>
  <p>The standard library's missing standard library.</p>
  <p>
    <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go" alt="Go 1.24+">
    <img src="https://img.shields.io/badge/deps-none-success" alt="Zero dependencies">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT">
    <img src="https://img.shields.io/github/v/tag/mirkobrombin/go-foundation?label=version" alt="version">
  </p>
  <p>
    <a href="https://go-foundation.bromb.in"><strong>Read the Documentation</strong></a>
  </p>
</div>

---

## Packages

| Package | Description |
|---------|-------------|
| `hosting` | Application host with ConfigureServices / ConfigureWeb / DI / auto-start |
| `srv` | Minimal API server with routing, middleware, model binding, auth, validation, rate limit |
| `di` | Typed dependency injection (Singleton/Scoped/Transient) |
| `configuration` | Multi-source config (env, file, flags) |
| `options` | Functional options, `Options[T]`, merge, validation |
| `scheduler` | Cron-based background jobs, fire-and-forget, delayed |
| `caching` | `Cache[T]` + `DistributedCache` interface + `DistributedBridge[T]` |
| `serializer` | JSON serialization policy (SnakeCase, CamelCase, custom types) |
| `telemetry` | Unified Tracer/Span/Meter/Counter/Histogram/Gauge (OTel-ready) |
| `testutil` | `TestHost` (DI + HTTP test server), `FakeLogger`, `TestResponse` |
| `validation` | Struct tag validation (required, email, min/max) |
| `pipeline` | Generic middleware pipeline |
| `health` | Health check registry |
| `events` | Type-safe event bus (middleware, wildcard, async) |
| `tracing` | `Tracer`/`Span` interface + noop |
| `pooling` | Generic `Pool[T]` with finalizer |
| `errutil` | `Auto()`, `Wrap()`, `WError`, `Print()`, `Recover()`, `JoinErrors()` |
| `auth` | Token signing with key rotation (HMAC, RSA, ECDSA, EdDSA) |
| `guard` | ABAC authorization via struct tags |
| `relay` | Background job processor (pub/sub with context propagation) |
| `httpx` | HTTP client middleware (retry, circuit breaker, logging) |
| `logger` | Structured logging (console, CLEF, async) |
| `plugin` | Plugin registry + lifecycle + sandbox exec |
| `secrets` | Secret stores (memory, env, cipher, prefix, fallback) |
| `worker` | Fixed-size goroutine pool |
| `metrics` | Counter, Gauge, Histogram, Timer |
| `saga` | Saga pattern with compensation LIFO |
| `fsm` | Declarative finite state machine |
| `tags` | Generic struct tag parser (cached) |
| `hooks` | Lifecycle hook discovery + runner |
| `resiliency` | Circuit breaker, retry, rate limiter, bulkhead |
| `safemap` | Thread-safe map + sharded map with TTL |
| `collections` | Set, OrderedSet, Queue, MultiMap, BiMap |
| `lock` | Lock interface + in-memory implementation |
| `reflectutil` | String-to-type binding + struct population |
| `adapters` | Pluggable adapter registry |
| `result` | `Result[T]` monad |
| `ring` | Ring buffer (generic + byte) |
| `cpio` | CPIO newc reader/writer |
| `align` | Power-of-2 alignment |
| `contracts` | Zero-cost interface contract markers |
| `pointer` | Field offset registry |

## Panicking, but with style

```go
defer errutil.Auto() // put this once at the top of main()

// to get panics like this:
  panic: runtime error: invalid memory address or nil pointer dereference

  1. main.main()
     /app/main.go:37
          35  func main() {
          36    defer errutil.Auto()
     >>   37    serve()
          38  }

  2. main.serve
     /app/main.go:9
           7  func serve() {
           8    h := &handler{}
     >>    9    h.handleRequest()
          10  }

  3. main.(*handler).handleRequest
     /app/main.go:15
          13  
          14  func (h *handler) handleRequest() {
     >>   15    h.authMiddleware(func() { h.dbQuery("42") })
          16  }

  4. main.(*handler).authMiddleware
     /app/main.go:19
          17  
          18  func (h *handler) authMiddleware(next func()) {
     >>   19    token := h.extractToken()
          20    if token == "" {

  5. main.(*handler).extractToken
     /app/main.go:28
          26  func (h *handler) extractToken() string {
          27    var t *string
     >>   28    return *t    ← crash here
          29  }
```

## License

MIT

## Logo

Created following the [gopher style](https://go.dev/blog/gopher) using AI (because I am not an illustration at this level).
