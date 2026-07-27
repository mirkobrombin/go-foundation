# Foundation MCP server

`foundation mcp` serves Foundation knowledge and Foundation tooling over the
Model Context Protocol, on stdio. It exists for one reason: an assistant asked to
write Foundation code can be wrong in two ways, and both are avoidable.

It can invent an API. The server answers from a catalog extracted from the source
of the version it ships with, so a symbol that is not in the catalog does not
exist, and the assistant is told so.

It can invent a workflow. The server runs the real analyzer, the real generator,
the compiler and the tests, and returns what they said. "It works" stops being a
claim and becomes an output.

## Setup

```sh
go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest
```

Then register it with any MCP client:

```json
{
  "mcpServers": {
    "foundation": {
      "command": "foundation",
      "args": ["mcp"]
    }
  }
}
```

The server speaks stdio and needs no network access. Tool calls that do not name
a directory use the process working directory, or the one given with
`foundation mcp -workspace /path/to/project`.

## What the client receives

At initialisation the server sends instructions that state the rules of
engagement: look up symbols before writing them, read the declaration rules
before writing tags, verify before reporting success, read a diagnostic's cause
before silencing it, commit generated registries, and say plainly when a
relationship can only be checked at build time. Clients surface these to the
model, so the rules travel with the server rather than living in one assistant's
configuration.

## Tools

| Tool | Answers |
|---|---|
| `foundation_overview` | What Foundation is, the layers, the workflow, the rules, the other tools. Call it first. |
| `foundation_packages` | Every importable package with its layer and purpose. |
| `foundation_package_api` | The full exported API of a package: signatures, docs, struct fields with tags, methods. |
| `foundation_symbol` | Find a symbol by name. An empty result means it does not exist in this version. |
| `foundation_declaration_rules` | The exact grammar for handlers, actions, injection, contracts, binding, errors, typed APIs, layering, scheduling, generation, the workflow, and v1 migration, with the mistakes each invites. |
| `foundation_checks` | Every diagnostic the analyzer can emit, with its cause and its fix. |
| `foundation_install` | The commands to install the module, the CLI, and this server. |
| `foundation_scaffold` | Write a project whose shapes are the ones the analyzer and generator expect. |
| `foundation_check` | Run the analyzer, return structured diagnostics. |
| `foundation_generate` | Write or verify the static registries. |
| `foundation_verify` | Build, analyse, check registries, vet, test. The gate before reporting work as done. |
| `foundation_migrate` | Where a v1 package went, and the behaviour changes an import rewrite misses. |

Prompts: `foundation_new_service`, `foundation_migrate_v1`, `foundation_review`.
Resources: the project documentation, embedded in the binary, served under
`foundation://docs/<name>`.

## How the answers stay aligned

The API catalog is extracted from the runtime module by `foundation catalog`,
which also embeds the documentation. Both are committed, and the pipeline runs
`foundation catalog -check`, so a change to the runtime API or to the docs that
is not reflected in the server fails the build. The alignment is enforced, not
promised.

The diagnostic catalog is held to the same standard by a test that compares it
against every report site in the analyzer: a new diagnostic that ships
undocumented fails the test.

## Regenerating

```sh
cd dev
go run ./cmd/foundation catalog        # rewrite the catalog and the embedded docs
go run ./cmd/foundation catalog -check # verify they match the source
```

## What it deliberately does not do

It does not fetch anything from the network, so it cannot serve a version other
than the one it was built from. It does not write application logic; scaffolding
produces the wiring shapes and leaves the domain to the author. It does not
claim that local analysis proves cross package wiring: `App.Build` remains the
boundary, and the tools say so rather than implying more.
