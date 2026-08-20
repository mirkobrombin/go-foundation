# WebAssembly plugins

Foundation has three plugin boundaries because they solve different problems.
`LoadSo` is the shortest path for Go code built with the same Go toolchain,
build flags, and dependency versions as its host. `ExecSandbox` starts a process
and exchanges JSON over standard streams. `LoadWasm` runs a WebAssembly module
inside the host process without binding the plugin to the host's Go ABI.

The WebAssembly path has two contracts:

- `abi/foundation-plugin.wit` describes the language-neutral operations.
- `abi/foundation_plugin.h` defines the current core WebAssembly representation.

WIT is the source-level contract. The runtime does not claim Component Model
support: wazero currently executes core WebAssembly modules, so Foundation maps
the WIT operations to functions, linear memory, and opaque byte payloads. A
future Component Model backend can keep the same operations without pretending
that the current binary format already is a component.

## Host setup

Load a module from bytes or from a file. A declared guest capability must have a
matching host handler at load time.

```go
store := plugin.CapabilityFunc(func(
    ctx context.Context,
    operation string,
    input []byte,
) ([]byte, error) {
    switch operation {
    case "put":
        return nil, save(ctx, input)
    case "get":
        return load(ctx, input)
    default:
        return nil, fmt.Errorf("unknown operation %q", operation)
    }
})

module, err := plugin.LoadWasmFile(ctx, "report.wasm",
    plugin.WithWasmCapability("reports.store", store),
    plugin.WithWasmMemoryLimit(32<<20),
    plugin.WithWasmPayloadLimit(2<<20),
    plugin.WithWasmCallTimeout(2*time.Second),
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

`Call` treats payloads as bytes. `CallJSON` is a convenience for a plugin whose
method contract is JSON.

```go
var result Report
err := module.CallJSON(ctx, "render", RenderRequest{ID: 42}, &result)
```

Calls are serialized per module. This keeps guest allocation and capability
responses deterministic. A capability handler must not call back into the same
module while it is handling a request.

## Metadata

The guest returns UTF-8 JSON from `foundation_metadata`:

```json
{
  "name": "reports",
  "version": "1.4.0",
  "description": "Renders reports",
  "methods": ["render"],
  "capabilities": ["reports.store"],
  "properties": {
    "language": "rust"
  }
}
```

Names use lowercase ASCII letters, digits after the first character, dots,
underscores, and hyphens. The host rejects duplicate methods and capabilities.
It also rejects the module if one declared capability has no handler.

`Metadata` returns a copy, so callers cannot alter the permissions or method set
after validation.

## ABI 1.0

The guest exports one memory named `memory` and these functions:

| Export | Core signature | Purpose |
|---|---|---|
| `foundation_abi_version` | `() -> i64` | ABI major in the high 32 bits, minor in the low 32 bits. |
| `foundation_alloc` | `(i32) -> i32` | Allocate a guest buffer for host input. |
| `foundation_free` | `(i32, i32)` | Release a buffer by pointer and length. |
| `foundation_metadata` | `() -> i64` | Return borrowed metadata as packed pointer and length. |
| `foundation_start` | `() -> i32` | Start the plugin. Zero means success. |
| `foundation_stop` | `() -> i32` | Stop the plugin. Zero means success. |
| `foundation_call` | `(i32, i32, i32, i32) -> i64` | Invoke a method with method and payload buffers. |
| `foundation_last_error` | `() -> i64` | Return a borrowed UTF-8 error as packed pointer and length. |

A packed buffer stores the pointer in the high 32 bits and length in the low 32
bits. `foundation_call` returns `UINT64_MAX` on failure. A successful call returns
a guest-owned buffer that stays valid until the host invokes `foundation_free`.
The output must not reuse either input buffer.

The ABI is compatible when the major version matches and the guest minor version
is not newer than the host minor version. A major change is a new contract. A
minor change may add optional behavior without changing existing signatures.

## Host capabilities

The runtime provides module `foundation` with these imports:

| Import | Core signature | Purpose |
|---|---|---|
| `host_call` | `(i32, i32, i32, i32, i32, i32) -> i32` | Call a named capability operation with a payload. |
| `host_response_len` | `() -> i32` | Read the successful response length. |
| `host_response_read` | `(i32, i32) -> i32` | Copy the response to guest memory. |
| `host_error_len` | `() -> i32` | Read the capability error length. |
| `host_error_read` | `(i32, i32) -> i32` | Copy the error to guest memory. |

`host_call` receives pointer and length pairs for capability, operation, and
payload. It returns one of the `WasmHostStatus` values. The guest asks for the
response or error length, allocates enough memory, then copies it. A read returns
the byte count. A negative value means the buffer was too small or invalid.

Capability names in metadata are part of the permission request. Importing
`host_call` does not grant anything by itself. The runtime checks the declaration
again on every call before invoking the handler.

## Isolation and limits

The default runtime allows 64 MiB of linear memory, 8 MiB per request or
response, and five seconds per call. `WithCloseOnContextDone` is enabled inside
wazero, so a guest that ignores cancellation is interrupted. Wazero closes that
module after an interrupted call; load a new instance before invoking it again.

The module receives no host resources by default:

- no filesystem;
- no inherited arguments or environment;
- no standard streams;
- no host clock or sleep;
- no network API.

Unknown imports and imported memories are rejected. WebAssembly validates guest
memory access, while Foundation validates every host-side pointer, length, name,
payload size, method, and capability.

## WASI Preview 1

Use `WithWasmWASI` only for a module compiled for WASI Preview 1. The option
installs the WASI functions but still inherits nothing. Add each resource
explicitly:

```go
module, err := plugin.LoadWasm(ctx, binary,
    plugin.WithWasmWASI(),
    plugin.WithWasmWASIArgs("reports-plugin", "--format=json"),
    plugin.WithWasmWASIEnv("LANG", "C.UTF-8"),
    plugin.WithWasmWASIFS(assets, "/assets"),
    plugin.WithWasmWASIStdout(logWriter),
    plugin.WithWasmWASIRandom(rand.Reader),
    plugin.WithWasmWASISystemClock(),
)
```

`WithWasmWASIFS` accepts `fs.FS`. The caller owns its isolation properties.
Passing `os.DirFS` over a directory with untrusted symlinks is not a security
boundary. Prefer a virtual filesystem or a host capability when the plugin only
needs a few file operations. Foundation exposes no socket option through WASI.

The plugin protocol never uses stdin or stdout for requests. WASI streams are
only resources for guest code that explicitly receives them.

## Discovery and shutdown

`DiscoverWasm` loads direct `.wasm` children of a directory in filename order.
If one module fails, every module loaded by that call closes before the error is
returned.

```go
registry := plugin.NewRegistry()
count, err := registry.RegisterWasmDirectory(ctx, "plugins",
    plugin.WithWasmCapability("reports.store", store),
)
if err != nil {
    return err
}
defer registry.CloseAll(ctx)

failures := registry.StartAll()
```

The registry owns modules registered by `RegisterWasmDirectory`. `CloseAll`
stops running plugins, then closes context-aware resources in reverse order.

## Building a guest

The C header can be used with Clang without a C runtime:

```sh
clang --target=wasm32 -O2 -nostdlib \
  -Wl,--no-entry -Wl,--export-memory -Wl,--allow-undefined \
  -Wl,--strip-all \
  -o report.wasm report.c
```

Rust, Zig, TinyGo, AssemblyScript, and other compilers can use the same exports
and imports. They do not need a Go SDK. Keep the exported signatures exact,
return metadata as UTF-8 JSON, and compile host imports under module
`foundation`. A WASI target is optional and only needed when the guest imports
WASI functions.

The C source and its compiled fixture live under `core/plugin/testdata`. Package
tests execute that fixture to cover lifecycle calls, opaque and JSON calls, host
capabilities, concurrent host calls, errors, deadlines, discovery, and registry
shutdown.
