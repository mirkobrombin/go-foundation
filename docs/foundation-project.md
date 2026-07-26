# Foundation v2 Project Shape

A Foundation service should expose its wiring in a small number of predictable
files. The static registry is generated from types, so route and action ownership
stays next to the code that implements it.

## Layout

```text
cmd/api/main.go
internal/config/config.go
internal/contracts/users.go
internal/handlers/users.go
internal/actions/users.go
internal/services/users.go
internal/jobs/cleanup.go
internal/testing/host.go
internal/handlers/zz_foundation.gen.go
internal/actions/zz_foundation.gen.go
```

`cmd/api/main.go` owns process startup. `internal/contracts` contains interfaces
shared across application packages. Handlers and actions declare Foundation
metadata on their concrete types. Services implement contracts with explicit
compiler-visible declarations.

Each package containing handlers or actions gets its own generated file. Do not
edit generated files.

## Startup

```go
package main

import (
    "log"

    "example.com/service/internal"
)

func main() {
    application, err := internal.Build()
    if err != nil {
        log.Fatal(err)
    }
    if err := application.Listen(":8080"); err != nil {
        log.Fatal(err)
    }
}
```

Application wiring stays in the package that owns generated registration:

```go
func Build() (*app.App, error) {
    application := app.New().
        Provide("users", services.NewUsers())

    handlers.RegisterFoundation(application)
    useractions.RegisterFoundation(application)
    if _, err := application.Build(); err != nil {
        return nil, err
    }
    return application, nil
}
```

## Contracts

Use a marker when the generator should own the assertion:

```go
type Users interface {
    Find(context.Context, int) (User, error)
}

type UserService struct {
    contracts.Implements[Users]
}
```

Use `contracts.Assert` for an explicit assertion without generation:

```go
var _ = contracts.Assert[Users]((*UserService)(nil))
```

Both patterns fail during development. The marker also gives the Foundation
analyzer and editor a stable relationship to display.

## HTTP handlers

```go
type GetUser struct {
    _     struct{} `method:"GET" path:"/users/{id:int}"`
    ID    int      `path:"id"`
    Users Users    `inject:"users"`
}

func (h *GetUser) Handle(ctx context.Context) (any, error) {
    return h.Users.Find(ctx, h.ID)
}
```

The analyzer checks method and path syntax, parameter fields, duplicate routes,
and local named dependency types. The generated registry supplies a static
constructor. `App.Build` validates dependency availability before serving.

## Actions

```go
type CreateUser struct {
    _     struct{} `action:"users.create" keys:"ctrl+n"`
    Name  string `json:"name"`
    Users Users `inject:"users"`
}

func (a *CreateUser) Handle(ctx context.Context) (any, error) {
    return a.Users.Create(ctx, a.Name)
}
```

Use typed actions when the caller and handler are both Go code and no string
metadata is required.

## Testing

Use `app/testing` for host-level tests and the focused package APIs for unit
tests:

```go
func newTestHost() *apptest.TestHost {
    return apptest.NewTestHost(func(builder *di.Builder) {
        builder.Provide("users", newFakeUsers())
    }, func(server *web.Server, container *di.Container) error {
        return server.RegisterDefinition(handlerDefinition, container)
    })
}
```

Run the same static steps locally and in CI:

```sh
foundation generate -check ./...
foundation check ./...
go test -race ./...
go vet ./...
```

## Reading order

New contributors should be able to understand a service in this order:

1. `cmd/api/main.go`
2. application `Build`
3. contracts
4. handlers and actions
5. services and jobs
6. tests

If routes, dependencies, or registrations require a runtime trace to discover,
move that relationship into a contract, typed key, or generated registry.
