# Foundation Project Shape

This guide shows how to shape a service that uses go-foundation without trying to use every package. Start with the runtime path first, then add support packages only when the service needs them.

## Layout

```text
cmd/api/main.go
internal/config/config.go
internal/handlers/health.go
internal/handlers/users.go
internal/services/users.go
internal/jobs/cleanup.go
internal/testing/host.go
```

`cmd/api/main.go` owns process startup. It wires configuration, services, HTTP handlers, jobs, health checks, and shutdown.

`internal/config` binds environment and file values into typed structs.

`internal/handlers` contains HTTP endpoint structs. Keep request binding tags close to the handler that consumes them.

`internal/services` contains application logic. HTTP, jobs, and dispatch handlers call this layer.

`internal/jobs` contains scheduled or delayed work.

`internal/testing` wraps `testutil.NewTestHost` with project defaults.

## Startup

```go
package main

import (
	"context"
	"log"

	"github.com/mirkobrombin/go-foundation/pkg/app"
)

func main() {
	a := app.New()

	a.Provide("users", NewUserService())
	a.RegisterHTTP(&GetUser{})
	a.Schedule("cleanup", "*/5 * * * *", cleanup)

	if err := a.Listen(":8080"); err != nil {
		log.Fatal(err)
	}
}

func cleanup(ctx context.Context) error {
	return nil
}
```

Keep `main` small. If wiring grows, move it to a `buildApp` function and keep package registration in one place.

## Handler Shape

```go
type GetUser struct {
	_     struct{} `method:"GET" path:"/users/{id:int}"`
	ID    int      `path:"id"`
	Users *UserService `inject:"users"`
}

func (h *GetUser) Handle(ctx context.Context) (any, error) {
	return h.Users.Get(ctx, h.ID)
}
```

Handlers should bind input, call services, and return a response. Put branching business rules in services, not in endpoint structs.

## Testing Shape

```go
func newTestHost() *testutil.TestHost {
	return testutil.NewTestHost(func(b *di.Builder, app *srv.Server) {
		di.RegisterInstance[*UserService](b, NewUserService())
		app.RegisterHandler(&GetUser{}, b.MustBuild())
	})
}
```

Use one project helper for test hosts so every package gets the same DI setup, routes, middleware, and cleanup behavior.

## Package Choice

Use these first:

- `app`, `hosting`, `srv`: runtime and HTTP.
- `di`: service wiring.
- `configuration`: typed config.
- `bind`, `validation`: request input.
- `scheduler`: background jobs.
- `health`: readiness checks.
- `testutil`: HTTP and DI tests.

Add the rest only when the service has that problem:

- `caching` for process or backend caches.
- `events`, `relay`, `dispatcher` for in-process or brokered work.
- `saga`, `fsm` for workflows with state.
- `secrets` for environment, encrypted, or Vault-backed secrets.
- `telemetry` for metrics, exporters, and provider-level instrumentation.
- `tracing` for small packages that only need span boundaries.

## Rule of Thumb

A foundation-based service should be readable in this order:

1. `cmd/api/main.go`
2. `internal/config`
3. `internal/handlers`
4. `internal/services`
5. `internal/jobs`
6. `internal/testing`

If a new contributor cannot find startup, routes, services, and tests in that order, fix the project shape before adding more packages.
