# Foundation for Go

This VS Code extension adds Foundation-specific information on top of gopls:

- diagnostics from `foundation check`;
- navigation across the contract graph, in both directions;
- navigation between `inject:"name"` and `Provide("name", value)`, in both directions;
- navigation from a route or action to the code that registers it;
- CodeLens and hover text for routes, actions, injected dependencies, and contracts;
- a command for deterministic registry generation.

## Navigation

Foundation relationships are written as type parameters and struct tags, which
gopls cannot follow. This extension resolves them like a compiled reference:

| From | Action | Goes to |
|---|---|---|
| `contracts.Implements[UserStore]` | Go to Definition | the `UserStore` declaration |
| `contracts.Assert[UserStore]((*Cached)(nil))` | Go to Definition | the `UserStore` declaration |
| `type UserStore interface` | Go to Implementations, or click the CodeLens | a peek list of every type declaring that contract |
| an implementing type name | Go to Implementations | the contracts it declares |
| `inject:"users"` | Go to Definition | the matching `Provide("users", ...)` |
| `Provide("users", ...)` | Find References | every field injecting that name |
| a route or action tag | click the CodeLens | where the type is registered |

An interface that has implementations carries a CodeLens with their count, for
example `Foundation: 2 implementations`. Clicking it opens the reference peek:
the list of implementations on one side, the code of the selected one on the
other. Enter or a double click opens an entry in the editor, Escape closes the
peek. A list is never collapsed into a jump, not even when it holds one entry,
because the point of asking is seeing everything that answers.

`contracts.Assert` markers count as implementations, so generated and manually
wired types appear in the same list. When a route has no registration, the
extension says so instead of failing silently: it usually means `foundation
generate` has not run yet.

Install the development CLI from the repository checkout:

```sh
(cd ../../dev && go install ./cmd/foundation)
```

Open this directory in VS Code and run `Developer: Install Extension from Location`
while developing the extension. A packaged Marketplace release can use the same
source without runtime dependencies.

Use `Foundation: Check Workspace` for an explicit check or enable
`foundation.checkOnSave`. The extension expects `foundation` in `PATH` unless
`foundation.executable` points to another binary.

The executable setting has machine scope. The extension is disabled for
untrusted workspaces because checks and generation run a local process.
