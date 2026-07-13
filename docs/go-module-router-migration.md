# go-module-router Migration

`go-module-router` is replaced by go-foundation for new projects. The old module can stay available for existing users, but new work should use the Foundation packages below.

## Package Map

| go-module-router | go-foundation |
|------------------|---------------|
| `pkg/transport/http` | `pkg/app`, `pkg/srv` |
| `pkg/core.Container` | `pkg/di` |
| `pkg/core.Binder` | `pkg/bind`, `pkg/reflectutil` |
| `pkg/swagger` | `pkg/openapi` |
| `pkg/transport/action` | `pkg/actions` |
| `pkg/transport/relay` | `pkg/relay` |
| `pkg/logger` | `pkg/logger` |

## HTTP

Old:

```go
r := router.New()
r.Provide("Users", users)
r.Register(&GetUser{})
r.Listen(":8080")
```

New:

```go
a := app.New()
a.Provide("Users", users)
a.RegisterHTTP(&GetUser{})

if err := a.Listen(":8080"); err != nil {
	log.Fatal(err)
}
```

Handlers keep the same shape:

```go
type GetUser struct {
	_     struct{}     `method:"GET" path:"/users/{id}"`
	ID    string       `path:"id"`
	Users *UserService `inject:"Users"`
}

func (h *GetUser) Handle(ctx context.Context) (any, error) {
	return h.Users.Get(ctx, h.ID)
}
```

## Actions

Old:

```go
type SaveAction struct {
	Meta     core.Pattern `action:"file.save" keys:"ctrl+s"`
	Document *Document
}

func (a *SaveAction) Handle(ctx context.Context) (any, error) {
	return a.Document.Save()
}
```

New:

```go
type SaveAction struct {
	_        struct{}  `action:"file.save" keys:"ctrl+s"`
	Document *Document `inject:"Document"`
	Name     string    `json:"name"`
}

func (a *SaveAction) Handle(ctx context.Context) (any, error) {
	return a.Document.Save(a.Name)
}
```

Standalone:

```go
r := actions.New()
r.Provide("Document", doc)
r.Register(&SaveAction{})

result, err := r.Dispatch(ctx, "file.save", map[string]any{
	"name": "notes.md",
})
```

Through `app`:

```go
a := app.New()
a.Provide("Document", doc)
a.RegisterActionHandler(&SaveAction{})

result, err := a.Dispatch(ctx, "file.save", map[string]any{
	"name": "notes.md",
})
```

Key bindings:

```go
result, err := a.DispatchKey(ctx, "ctrl+s", map[string]any{
	"name": "notes.md",
})
```

Optional events:

```go
bus := events.New()
a.UseActionEvents(bus)
```

Use `UseAsyncActionEvents` when event handlers should run through the async event queue.

## Relay

`go-module-router` had a relay transport that read `relay:"topic"` tags from handler structs. Foundation keeps relay registration explicit:

```go
r := manager.New()

manager.Register[SendNotification](r, "notifications.send", func(ctx context.Context, payload SendNotification) error {
	return notifications.Send(ctx, payload.UserID, payload.Message)
})

ready, err := r.Start(ctx)
if err != nil {
	log.Fatal(err)
}
<-ready
```

Use this form for new code. It keeps the payload type visible at registration and avoids a second dispatch layer around the broker.

## OpenAPI

Old:

```go
doc, err := swagger.Build("API", "1.0.0", &GetUser{})
```

New:

```go
doc, err := openapi.Build("API", "1.0.0", &GetUser{})
```

`OpenAPIMeta()` is still supported.

## Deprecation Message

Suggested README notice for `go-module-router`:

```md
go-module-router is superseded by go-foundation.

HTTP routing, declarative handlers, DI, OpenAPI, actions, and relay support now live in go-foundation. Existing projects can keep using this module, but new projects should start from go-foundation.
```
