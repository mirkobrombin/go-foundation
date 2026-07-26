# Foundation Doctor

Foundation Doctor is an opt-in startup check for foundation-based services. It is compiled into the application only when the `run_foundation_doctor` build tag is set.

```sh
go build -tags run_foundation_doctor -o api ./cmd/api
FOUNDATION_DOCTOR=fail ./api
```

Without the tag, startup keeps the normal path.

## Modes

`FOUNDATION_DOCTOR` controls what happens at startup:

| Value | Behavior |
|-------|----------|
| `off` | Skip checks. |
| `print` or empty | Print a text report and continue. |
| `json` | Print a JSON report and continue. |
| `fail` | Print a text report and stop startup when a failure is found. |

Unknown values are treated like `print`.

## Checks

The first version checks the HTTP route table exposed by `web.Server`:

- At least one route is registered.
- No duplicate `METHOD path` pair exists.
- A liveness route exists at `GET /health` or `GET /health/live`.
- A readiness route exists at `GET /health/ready`.

Missing routes are failures. Missing health routes are warnings, because some services are not HTTP-first.

## Automatic Integration

The build tag activates startup hooks in `app` and `app/web`.

```go
a := app.New()
a.RegisterHTTP(&GetUser{})

if err := a.Listen(":8080"); err != nil {
	log.Fatal(err)
}
```

With `run_foundation_doctor`, `app.Listen` runs the doctor after `Build` has registered handlers and before the HTTP host starts.

The same tag also runs the doctor when a project starts `web.Server` directly:

```go
server := web.New()
server.MapGet("/health/live", live)
server.MapGet("/health/ready", ready)

log.Fatal(server.ListenAndServe(":8080"))
```

`hosting.ConfigureWeb` uses `web.Server`, so that path is covered too.

## Direct Use

Projects that bypass Foundation startup and pass a handler to `net/http` directly can call the package themselves.

```go
report := doctor.CheckSource(doctor.Source{
	Routes: []doctor.Route{
		{Method: "GET", Path: "/health/live"},
		{Method: "GET", Path: "/health/ready"},
	},
})

if report.Failures() > 0 {
	os.Exit(1)
}
```
