<div align="center">
  <img src="https://github.com/mirkobrombin/go-foundation/blob/main/logo.png?raw=true" height="128"/>
  <h1>go-foundation v2</h1>
  <p>A Go application foundation with development-time checks.</p>
  <p>
    <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go" alt="Go 1.25+">
    <img src="https://img.shields.io/badge/runtime_deps-wazero-success" alt="wazero runtime dependency">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT">
  </p>
</div>

Foundation is the plumbing an application needs before it is an application:
dependency injection, HTTP, actions, scheduling, configuration, caching,
logging, resiliency. What v2 changes is *when* a mistake surfaces.

An application built this way says things the Go compiler cannot check. A
contract is a type parameter, a route is a struct tag, an injected dependency is
a name in a string. In v1 those relationships were resolved by reflection while
the program ran, so a wrong one became a failed request. In v2 an analyzer reads
them while the file is open, a generator turns them into compile-time assertions
and static registries, and the editor navigates them like ordinary symbols.

## Install

```sh
go get github.com/mirkobrombin/go-foundation/v2@latest
go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest
```

The runtime uses wazero for isolated WebAssembly plugins. It is pure Go and does
not require CGO. The analyzer, the generator, and the CLI live in a separate
module, so an application that imports Foundation never pulls the analysis
packages into its own build.

## Static workflow

Declare contracts and application metadata in normal Go:

```go
type UserStore interface {
    Find(int) (User, bool)
}

type MemoryUserStore struct {
    contracts.Implements[UserStore]
}

type GetUser struct {
    _     struct{} `method:"GET" path:"/users/{id:int}"`
    ID    int      `path:"id"`
    Users UserStore `inject:"users"`
}

func build() (*app.App, error) {
    application := app.New().Provide("users", NewMemoryUserStore())
    RegisterFoundation(application)
    _, err := application.Build()
    return application, err
}
```

Generate registration code and check the complete workspace:

```sh
foundation generate ./...
foundation check ./...
go test ./...
```

Generation writes one `zz_foundation.gen.go` per package that declares
something, holding the contract assertions and the static constructors, and
removes the file again when the declarations are gone. Commit it: that is what
makes a broken contract a compile error on a machine that has never installed
the CLI.

Generation is a choice, not a toll. `RegisterHTTP` and `RegisterActionHandler`
still register by reflection, and `foundation check` reports the same problems
either way, so a v1 application can move to v2 without a single generated file
in its tree.

See the [quickstart example](examples/quickstart/quickstart.go), exercised by its
own test.

Calling `App.Listen("")` binds to `127.0.0.1:8080`. Pass an explicit public
address only when the service is intended to accept remote traffic. Use
`App.ListenTLS` for direct HTTPS, or terminate TLS at a trusted reverse proxy.

## Layers

| Layer | Purpose | Examples |
|---|---|---|
| `core` | Runtime building blocks with no application dependency | contracts, caching, validation, configuration, events, telemetry |
| `app` | Application composition and boundaries | DI, HTTP, actions, dispatcher, hosting, testing |
| `dev` | Development-only analysis and generation | `foundation check`, `foundation generate` |
| `editors` | Editor-specific presentation | VS Code diagnostics, CodeLens, hover, contract and dependency navigation |

The analyzer enforces the dependency direction: `core` cannot import `app`, and
runtime packages cannot import `dev`.

## Development-time checks

`foundation check` reports:

- invalid `contracts.Implements[T]` declarations;
- invalid DI implementation and constructor registrations;
- duplicate or malformed routes and actions;
- route parameters without matching fields;
- locally missing or mistyped named dependencies;
- dispatches to locally unknown actions;
- invalid literal scheduler registrations;
- ignored binding errors;
- forbidden layer imports.

The compiler remains the authority for generated contract assertions. `go vet`,
tests, and the race detector remain part of the expected verification path.

## Editor navigation

A contract is a type parameter and a dependency is a struct tag, so a Go editor
cannot follow either on its own. The VS Code extension reads them and answers
the two questions that matter while writing code: what implements this, and
where does this come from.

![Implementations of a contract shown in the VS Code peek view](docs/images/vscode-implementations.png)

The screenshot is the quickstart example. `UserStore` carries a CodeLens with
the number of implementations found in the workspace, and clicking it opens the
peek: the implementations on one side, the code of the selected one on the
other. `MemoryUserStore` declares the relationship with
`contracts.Implements[UserStore]`, which is also a CodeLens leading back to the
interface. The same navigation works between `inject:"users"` and
`Provide("users", ...)`, in both directions.

## Assistants

`foundation mcp` serves the same knowledge and the same tools over the Model
Context Protocol, so an assistant answers from the API catalog of the version it
is working with instead of from memory, and verifies with the real analyzer,
generator, compiler and tests instead of asserting.

```json
{
  "mcpServers": {
    "foundation": { "command": "foundation", "args": ["mcp"] }
  }
}
```

The catalog is extracted from the source and checked in the pipeline, so the
answers cannot drift from the code. See [the MCP server](docs/mcp.md).

## Typed APIs

Named dependency keys can retain their Go type:

```go
var usersKey = di.NewKey[UserStore]("users")

builder := di.NewBuilder()
di.ProvideKey(builder, usersKey, store)
container, err := builder.Build()
users, ok := di.ResolveKey(container, usersKey)
```

Actions also have a typed path:

```go
router := actions.New()
create := actions.NewTyped[CreateUser, User]("users.create")
err := actions.HandleTyped(router, create, handler)
result, err := actions.DispatchTyped(ctx, router, create, payload)
```

Use strings where they are part of external metadata. Use typed APIs inside Go
code when the compiler can carry the relationship.

## Plugins

Foundation supports three plugin boundaries. Go shared objects load existing
Go plugins that were built with the same toolchain and dependency graph.
`ExecSandbox` keeps a plugin in a separate process with a JSON protocol.
WebAssembly plugins use a stable Foundation ABI, run in-process through wazero,
and can be written in any language that produces a core Wasm module.

```go
module, err := plugin.LoadWasmFile(ctx, "report.wasm",
    plugin.WithWasmCapability("reports.store", storeCapability),
    plugin.WithWasmMemoryLimit(32<<20),
)
if err != nil {
    return err
}
defer module.Close(ctx)

if err := module.StartContext(ctx); err != nil {
    return err
}
result, err := module.Call(ctx, "render", request)
```

A module inherits no filesystem, process environment, clock, stream, or network
access. Host functions are exposed as named capabilities and a plugin must both
declare and receive each one before it can load. WASI Preview 1 is available as
an explicit option for code that needs it. See [WebAssembly plugins](docs/plugins.md)
for the ABI, language contract, discovery, limits, and WASI configuration.

## Documentation

- [Project shape](docs/foundation-project.md)
- [Development tools](docs/development-tools.md)
- [MCP server](docs/mcp.md)
- [WebAssembly plugins](docs/plugins.md)
- [V2 migration](docs/v2-migration.md)
- [Foundation Doctor](docs/foundation-doctor.md)
- [go-module-router migration](docs/go-module-router-migration.md)
- [VS Code extension](editors/vscode/README.md)

## License

MIT
