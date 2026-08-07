# Foundation Development Tools

Foundation v2 keeps runtime and development dependencies separate. The root
module contains application code. `dev` is an independent Go module containing
the analyzer, generator, and CLI.

## CLI

Install the CLI:

```sh
go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest
```

The tools module carries its own tags, `dev/vX.Y.Z`, so `@latest` resolves
against them and not against the runtime module. To run an unreleased change,
install from a checkout instead:

```sh
(cd dev && go install ./cmd/foundation)
```

Check packages:

```sh
foundation check ./...
```

Generate static contract assertions and registries:

```sh
foundation generate ./...
```

Verify that committed generated files match the source:

```sh
foundation generate -check ./...
```

Scan dependencies and source for known vulnerabilities:

```sh
foundation audit
foundation audit -sbom sbom.json -json
```

Generation writes `zz_foundation.gen.go` atomically and formats it with the Go
formatter. Output is sorted by package path and source declaration order.

## Supply chain audit

`foundation audit` reads every dependency manifest it recognises, matches the
dependencies against the live vulnerability databases, and scans the source for
risky patterns. The scanner is EUProvGuard, imported as a library: there is no
binary to find on PATH, nothing to keep in step with a separate release, and no
output to parse back into structure.

It exits non-zero when it finds something, and also when it could not finish.
Vulnerability matching needs the network, and a scan that could not reach the
databases reports zero findings for a reason that is not "nothing is wrong".
Those cases are listed as incomplete, and `-offline` always reports as such.

It is a separate command from `foundation check` on purpose. The analyzer is
static, local, and fast enough to run on every save. The audit reads the network
and walks the whole tree. Behind one command, the fast check would become slow
and the offline one would start needing the network.

## Analyzer scope

The analyzer uses Go type information, not text matching, for contracts and DI
generic calls. It also interprets Foundation struct tags and local string-key
wiring. Literal scheduler registrations are checked for empty or duplicate names,
invalid five-field cron expressions, and nil handlers.

Some wiring can span packages or be assembled conditionally. The analyzer does
not claim that local analysis can prove those cases. `App.Build` and
`Container.ValidateTarget` keep a deterministic build boundary for relationships
that remain dynamic.

Tests that intentionally contain invalid Foundation declarations can use:

```go
//foundation:ignore-file
```

A single contract declaration can be ignored with:

```go
// foundation:ignore contract
type IntentionalInvalidFixture struct {
    contracts.Implements[SomeContract]
}
```

OpenAPI-only route schemas that intentionally do not implement `web.Handler` can
use `// foundation:ignore handler` on the type declaration.

Use ignore directives only for deliberate compatibility or schema fixtures.

## Generated APIs

For packages containing declarative handlers or actions, generation creates:

```go
func FoundationHTTPHandlers() []web.HandlerDefinition
func FoundationActions() []actions.Definition
func RegisterFoundation(application *app.App)
```

For each `contracts.Implements[T]` marker, generation creates a normal Go
assignment assertion. Removing a required method then fails compilation even if
the analyzer is not installed.

## VS Code

The extension under `editors/vscode` adds:

- diagnostics from the CLI;
- CodeLens and hover details for Foundation metadata;
- definition, implementation, and reference lookup across the contract graph and
  named dependencies, in both directions;
- navigation from a route or action to the code that registers it;
- check and generate commands.

Contract navigation reads `contracts.Implements[T]` and `contracts.Assert[T]`
markers, so an interface reports every implementation, and an implementation
reports the contracts it declares. Dependency navigation works from the injected
field to the provider and from the provider back to every injected field.

gopls continues to own Go symbols, method sets, references, and compiler
diagnostics. The extension adds only Foundation relationships that gopls cannot
infer from tags and registration strings.

Run its tests without installing npm packages:

```sh
cd editors/vscode
node --check extension.js
node --test
```

## CI

A v2 pipeline should treat these as separate gates:

```sh
go test -race ./...
go test -race -tags run_foundation_doctor ./...
go vet ./...
go vet -tags run_foundation_doctor ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
(cd dev && go test -race ./... && go vet ./...)
(cd dev && go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...)
(cd dev && go build -o ../foundation ./cmd/foundation)
./foundation check ./...
./foundation generate -check ./...
```

The checked-in workflow also tests the editor parsing core with Node.
