# go-foundation

A collection of foundational Go primitives for building declarative, struct-tag-driven libraries.

This SDK provides shared building blocks used across my ecosystem, including:
- **Struct Tag Parsing**: Generic `key:value` tag parser
- **Dependency Injection**: Minimal, type-safe DI container
- **Adapter Pattern**: Pluggable backend registry
- **Hook Discovery**: Automatic lifecycle hook detection

## Installation

```bash
go get github.com/mirkobrombin/go-foundation
```

## Modules

### `pkg/tags` - Struct Tag Parser

Generic parser for struct tags with `key:value` syntax.

```go
import "github.com/mirkobrombin/go-foundation/pkg/tags"

p := tags.NewParser("guard", tags.WithPairDelimiter(";"))
result := p.Parse("role:owner; read:admin,user")
// result["role"] = ["owner"]
// result["read"] = ["admin", "user"]
```

### `pkg/di` - Dependency Injection

Minimal DI container with generics support, contract validation, and discovery.

```go
import "github.com/mirkobrombin/go-foundation/pkg/di"

c := di.New()
c.Provide("db", myDB)

db := di.Get[*sql.DB](c, "db")

// Find all instances implementing an interface
workers := di.ResolveAll[Worker](c)
```

### `pkg/contracts` - Explicit Interface Contracts

Zero-cost markers to declare interface implementation intent, enabling IDE discovery and runtime validation.

```go
import "github.com/mirkobrombin/go-foundation/pkg/contracts"

type MyService struct {
    contracts.Implements[IService] // IDE knows this implements IService
    db *sql.DB
}

// Validates all declared contracts via reflection
contracts.MustVerify(&MyService{})
```

### `pkg/adapters` - Pluggable Backends

Generic registry for swappable adapters/backends.

```go
import "github.com/mirkobrombin/go-foundation/pkg/adapters"

r := adapters.NewRegistry[Transport]()
r.Register("http", httpTransport)
r.Register("grpc", grpcTransport)
r.SetDefault("http")

t := r.Default()
```

### `pkg/hooks` - Lifecycle Hooks

Automatic discovery of lifecycle methods via reflection.

```go
import "github.com/mirkobrombin/go-foundation/pkg/hooks"

d := hooks.NewDiscovery()
methods := d.Discover(myStruct, "OnEnter")
// Returns map of "OnEnterPaid", "OnEnterCancelled", etc.
```

### `pkg/options` - Functional Options

Generic functional options pattern.

```go
import "github.com/mirkobrombin/go-foundation/pkg/options"

type Config struct { Host string; Port int }

func WithHost(h string) func(*Config) { return func(c *Config) { c.Host = h } }

cfg := &Config{}
options.Apply(cfg, WithHost("localhost"))
```

### `pkg/safemap` - Thread-Safe Map

Generic concurrent map with helpers.

```go
import "github.com/mirkobrombin/go-foundation/pkg/safemap"

m := safemap.New[string, int]()
m.Set("count", 1)
m.Compute("count", func(v int, _ bool) int { return v + 1 })
```

### `pkg/result` - Result Type

Functional error handling.

```go
import "github.com/mirkobrombin/go-foundation/pkg/result"

r := result.Try(func() (int, error) { return strconv.Atoi("123") })
doubled := result.Map(r, func(n int) int { return n * 2 })
fmt.Println(doubled.UnwrapOr(0)) // 246
```

### `pkg/resiliency` - Resiliency Patterns

Circuit Breaker and Retry with exponential backoff.

```go
import "github.com/mirkobrombin/go-foundation/pkg/resiliency"

cb := resiliency.NewCircuitBreaker(3, time.Minute)
err := cb.Execute(func() error { return doWork() })

err := resiliency.Retry(ctx, func() error { return doWork() }, resiliency.WithAttempts(5))
```

### `pkg/lock` - Locking Primitives

Common interfaces for distributed or local locking.

```go
import "github.com/mirkobrombin/go-foundation/pkg/lock"

// Use with your Redis/Etcd locker implementation
func Process(l lock.Locker) {
    l.Acquire(ctx, "resource-1", time.Second)
    defer l.Release(ctx, "resource-1")
}
```

### `pkg/collections` - Generic Collections

Thread-safe generic collections like `Set`.

```go
import "github.com/mirkobrombin/go-foundation/pkg/collections"

s := collections.NewSet[string]()
s.Add("item-1", "item-2")
if s.Has("item-1") { ... }
```

### `pkg/errors` - Error Utilities

Aggregation and grouping of multiple errors.

```go
import "github.com/mirkobrombin/go-foundation/pkg/errors"

errs := &errors.MultiError{}
errs.Append(err1, err2)
return errs.ErrorOrNil()
```

### `pkg/reflect` - Reflection Helpers

Universal string-to-type binder.

```go
import "github.com/mirkobrombin/go-foundation/pkg/reflect"

var count int
reflect.Bind(reflect.ValueOf(&count).Elem(), "42")
```

### `pkg/cpio` - CPIO (newc) Reader/Writer

Portable CPIO newc pack/unpack primitives, useful for initramfs/tooling.

```go
import "github.com/mirkobrombin/go-foundation/pkg/cpio"

var buf bytes.Buffer
_ = cpio.PackDir("./rootfs", &buf, cpio.WithMTimeUnix(0))
```

### `pkg/ring` - Ring Buffers

Low-level non-thread-safe ring buffers (generic + byte-specialized).

```go
import "github.com/mirkobrombin/go-foundation/pkg/ring"

b := ring.New[int](128)
_ = b.Push(1)
```

### `pkg/align` - Alignment Helpers

Align up/down to power-of-two boundaries.

```go
import "github.com/mirkobrombin/go-foundation/pkg/align"

_ = align.Up[uint64](123, 64) // 128
```

## Why go-foundation?

This library consolidates patterns that were duplicated across multiple of my projects, I just
thought it would be a good idea to have a shared library for these primitives.

## Migration from go-struct-flags

`go-struct-flags` is now deprecated. Replace:

```go
// Before
import "github.com/mirkobrombin/go-struct-flags/v2/pkg/binder"

// After
import "github.com/mirkobrombin/go-foundation/pkg/tags"
```

as for now, they are fully compatible and can be used interchangeably.

## License

MIT License. See [LICENSE](LICENSE) for details.
