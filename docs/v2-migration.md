# Migrating to Foundation v2

V2 changes the module path, package layout, registration defaults, and several
error-handling contracts. Treat the upgrade as an application wiring migration,
not a search-only import update.

## Import paths

The module path is now:

```text
github.com/mirkobrombin/go-foundation/v2
```

Common package moves:

| V1 | V2 |
|---|---|
| `pkg/app` | `app` |
| `pkg/di` | `app/di` |
| `pkg/actions` | `app/actions` |
| `pkg/dispatcher` | `app/dispatcher` |
| `pkg/hosting` | `app/hosting` |
| `pkg/srv` | `app/web` |
| `pkg/testutil` | `app/testing` |
| other `pkg/<name>` packages | `core/<name>` |

`srv` is now package `web`. `testutil` is now package `apptest`.

## Recommended sequence

1. Change the module requirement and imports.
2. Rename `srv` and `testutil` package references.
3. Handle returned errors from handler registration and injection.
4. Make every injected field explicit with `inject:"name"`.
5. Replace dynamic handler and action registration with generated definitions.
6. Add contract markers or explicit assertions.
7. Run generator, analyzer, tests, race tests, and vet.

## Behavior changes

Named injection no longer includes untagged fields. Missing dependencies, wrong
types, duplicate providers, duplicate routes, duplicate actions, and constructor
errors fail at build or registration time.

`Container.Inject` now returns an error:

```go
if err := container.Inject(target); err != nil {
    return err
}
```

Static definitions return registration errors:

```go
if err := server.RegisterDefinition(definition, container); err != nil {
    return err
}
```

JSON request binding rejects unknown fields, malformed trailing values, and
bodies larger than 1 MiB. Binding failures return client error status codes
instead of becoming internal server errors.

Action payload binding rejects unknown fields. Typed actions are available when
the relationship does not need a string name at the call site.

## Static registration

Existing reflection-based APIs remain available as a migration path:

```go
application.RegisterHTTP(&GetUser{})
application.RegisterActionHandler(&CreateUser{})
```

The v2 target is:

```go
application := app.New().Provide("users", store)
RegisterFoundation(application)
```

Run:

```sh
foundation generate ./...
foundation check ./...
```

Commit generated files so normal compilation and code review show the complete
registry.

## Release tags

The runtime and development tools are separate Go modules. A v2 release needs
both tags:

```text
v2.0.0
dev/v2.0.0
```

The nested module tag is required before
`go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@v2.0.0`
can resolve outside a source checkout.

## Contract migration

V1 runtime marker:

```go
type Service struct {
    contracts.Implements[Store]
}
```

The same marker is supported in v2, but the analyzer validates it and the
generator emits a compile-time assertion. For Foundation internals or manually
wired types, an explicit assertion is enough:

```go
var _ = contracts.Assert[Store]((*Service)(nil))
```

Do not rely on `contracts.Verify` as the primary check. It remains for dynamic
plugin boundaries and compatibility code.
