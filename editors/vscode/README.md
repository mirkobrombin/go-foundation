# Foundation for Go

This VS Code extension adds Foundation-specific information on top of gopls:

- diagnostics from `foundation check`;
- CodeLens and hover text for routes, actions, injected dependencies, and contracts;
- definition and reference lookup between `inject:"name"` and `Provide("name", value)`;
- a command for deterministic registry generation.

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
